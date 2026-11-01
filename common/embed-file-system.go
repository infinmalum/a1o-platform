package common

import (
	"embed"
	"github.com/gin-contrib/static"
	"io/fs"
	"net/http"
	"strings"
)

// Credit: https://github.com/gin-contrib/static/issues/19

type embedFileSystem struct {
	http.FileSystem
}

// Exists checks the path relative to the embedded subtree, so a handler
// mounted at a non-root prefix (e.g. /admin) resolves its files correctly;
// gin-contrib's local variant joins the prefix instead, which is wrong for
// filesystems already rooted at the target.
func (e embedFileSystem) Exists(prefix string, path string) bool {
	if prefix != "" && prefix != "/" {
		path = strings.TrimPrefix(path, prefix)
	}
	_, err := e.Open(path)
	return err == nil
}

func EmbedFolder(fsEmbed embed.FS, targetPath string) static.ServeFileSystem {
	efs, err := fs.Sub(fsEmbed, targetPath)
	if err != nil {
		panic(err)
	}
	return embedFileSystem{
		FileSystem: http.FS(efs),
	}
}
