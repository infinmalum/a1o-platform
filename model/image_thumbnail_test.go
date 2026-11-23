package model

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageAssetPreviewAndOwnedDeletion(t *testing.T) {
	openImageGenTestDB(t)
	run, _, err := CreateImageGeneration(&ImageGeneration{UserId: 7, Prompt: "sample", IdempotencyKey: "preview"})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "original.png")
	file, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, png.Encode(file, image.NewRGBA(image.Rect(0, 0, 1024, 512))))
	require.NoError(t, file.Close())
	asset := &ImageAsset{UserId: 7, RunId: run.Id, Path: path, Mime: "image/png"}
	require.NoError(t, CreateImageAsset(asset))
	require.Error(t, DeleteCompletedImageAsset(asset.Id, 8))
	require.Error(t, DeleteCompletedImageAsset(asset.Id, 7))

	thumbnail, err := ReadImageThumbnail(asset)
	require.NoError(t, err)
	preview, err := jpeg.DecodeConfig(bytes.NewReader(thumbnail))
	require.NoError(t, err)
	require.Equal(t, 256, preview.Width)
	require.Equal(t, 128, preview.Height)
	cached, err := os.ReadFile(path + ".thumb.jpg")
	require.NoError(t, err)
	require.Equal(t, thumbnail, cached)
	results, err := GetImageAssetsForRuns(7, []int{run.Id})
	require.NoError(t, err)
	require.Len(t, results[run.Id], 1)
	foreign, err := GetImageAssetsForRuns(8, []int{run.Id})
	require.NoError(t, err)
	require.Empty(t, foreign)

	require.NoError(t, CompleteImageGeneration(run.Id, 10))
	require.NoError(t, DeleteCompletedImageAsset(asset.Id, 7))
	require.NoFileExists(t, path)
	require.NoFileExists(t, path+".thumb.jpg")
	_, err = GetImageAsset(asset.Id, 7)
	require.Error(t, err)
	stored, err := GetImageGeneration(run.Id, 7)
	require.NoError(t, err)
	require.Equal(t, 10, stored.ConsumeLogId)
}
