package model

import (
	"bytes"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const imageThumbnailEdge = 256
const imageThumbnailPixelLimit = 16_777_216

var imageThumbnailLock sync.Mutex

// ReadImageThumbnail lazily creates one bounded thumbnail on disk, including
// for assets saved before thumbnails were introduced.
func ReadImageThumbnail(asset *ImageAsset) ([]byte, error) {
	path := asset.Path + ".thumb.jpg"
	if thumbnail, err := os.ReadFile(path); err == nil {
		return thumbnail, nil
	}
	imageThumbnailLock.Lock()
	defer imageThumbnailLock.Unlock()
	if thumbnail, err := os.ReadFile(path); err == nil {
		return thumbnail, nil
	}
	file, err := os.Open(asset.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, _, err := image.DecodeConfig(file)
	if err != nil || info.Width < 1 || info.Height < 1 || info.Width > imageThumbnailPixelLimit/info.Height {
		return nil, nil
	}
	if _, err := file.Seek(0, 0); err != nil {
		return nil, err
	}
	source, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}
	width, height := info.Width, info.Height
	if edge := max(width, height); edge > imageThumbnailEdge {
		width = max(1, width*imageThumbnailEdge/edge)
		height = max(1, height*imageThumbnailEdge/edge)
	}
	thumbnail := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(thumbnail, thumbnail.Bounds(), source, source.Bounds(), draw.Over, nil)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, thumbnail, &jpeg.Options{Quality: 78}); err != nil {
		return nil, err
	}
	// Serving a thumbnail only requires a successful encode. Cache writes can
	// fail on a read-only mount without preventing preview.
	if temporary, err := os.CreateTemp(filepath.Dir(path), ".thumbnail-*"); err == nil {
		_, writeErr := temporary.Write(encoded.Bytes())
		closeErr := temporary.Close()
		if writeErr == nil && closeErr == nil {
			_ = os.Rename(temporary.Name(), path)
		}
		_ = os.Remove(temporary.Name())
	}
	return encoded.Bytes(), nil
}
