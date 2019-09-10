package cli

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/int128/gpup/photos"
)

func (c *CLI) upload(ctx context.Context) error {
	if len(c.Paths) == 0 {
		return fmt.Errorf("Nothing to upload")
	}
	uploadItems, md5s, err := c.findUploadItems()
	if err != nil {
		return err
	}
	if len(uploadItems) == 0 {
		return fmt.Errorf("Nothing to upload in %s", strings.Join(c.Paths, ", "))
	}
	log.Printf("The following %d items will be uploaded:", len(uploadItems))
	for i, uploadItem := range uploadItems {
		fmt.Fprintf(os.Stderr, "#%d: %s\n", i+1, uploadItem)
	}

	client, err := c.newOAuth2Client(ctx)
	if err != nil {
		return err
	}
	service, err := photos.New(client)
	if err != nil {
		return err
	}
	var results []*photos.AddResult
	switch {
	case c.AlbumTitle != "":
		results, err = service.AddToAlbum(ctx, c.AlbumTitle, uploadItems)
	case c.NewAlbum != "":
		results, err = service.CreateAlbum(ctx, c.NewAlbum, uploadItems)
	default:
		results = service.AddToLibrary(ctx, uploadItems)
	}
	if err != nil {
		return err
	}
	f, err := os.OpenFile(c.CacheName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
	}

	for i, r := range results {
		if r.Error != nil {
			fmt.Printf("#%d: %s: %s\n", i+1, uploadItems[i], r.Error)
		} else {
			fmt.Printf("#%d: %s: OK\n", i+1, uploadItems[i])
			if f != nil && md5s[i] != "" {
				if md5hash, err := hex.DecodeString(md5s[i]); err == nil {
					f.Write(md5hash)
				}
			}
		}
	}
	return nil
}

func (c *CLI) findUploadItems() ([]photos.UploadItem, []string, error) {
	client := c.newHTTPClient()
	uploadItems := make([]photos.UploadItem, 0)
	md5s := make([]string, 0)
	for _, arg := range c.Paths {
		switch {
		case strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://"):
			r, err := http.NewRequest("GET", arg, nil)
			if err != nil {
				return nil, nil, fmt.Errorf("Could not parse URL: %s", err)
			}
			if c.RequestBasicAuth != "" {
				kv := strings.SplitN(c.RequestBasicAuth, ":", 2)
				r.SetBasicAuth(kv[0], kv[1])
			}
			for _, header := range c.RequestHeaders {
				kv := strings.SplitN(header, ":", 2)
				r.Header.Add(strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1]))
			}
			uploadItems = append(uploadItems, &photos.HTTPUploadItem{
				Client:  client,
				Request: r,
			})
			md5s = append(md5s, "")
		default:
			if err := filepath.Walk(arg, func(name string, info os.FileInfo, err error) error {
				switch {
				case err != nil:
					return err
				case info.Mode().IsRegular():
					if needupload, md5 := c.NeedUpload(name); needupload {
						uploadItems = append(uploadItems, photos.FileUploadItem(name))
						md5s = append(md5s, md5)
					}
					return nil
				default:
					return nil
				}
			}); err != nil {
				return nil, nil, fmt.Errorf("Error while finding files in %s: %s", arg, err)
			}
		}
	}
	return uploadItems, md5s, nil
}

func (c *CLI) NeedUpload(name string) (bool, string) {
	f, err := os.Open(name)
	if err != nil {
		return false, ""
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, ""
	}
	md5 := fmt.Sprintf("%x", h.Sum(nil))
	if _, ok := c.FileHash[md5]; ok {
		return false, md5
	}
	c.FileHash[md5] = true
	return true, md5
}
