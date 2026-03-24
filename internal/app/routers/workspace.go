package routers

// 本文件实现 /api/workspace/download|upload：将整个工作区目录打包为 zip 下载，或从 zip 安全解压回工作区。

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/willisdu/gopaw/internal/config"
)

type WorkspaceController struct{}

func zipDir(root string) ([]byte, error) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			_, err = zw.Create(rel + "/")
			return err
		}
		h, err := zip.FileInfoHeader(mustStat(path))
		if err != nil {
			return err
		}
		h.Name = rel
		h.Method = zip.Deflate
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func mustStat(p string) os.FileInfo {
	info, _ := os.Stat(p)
	return info
}

func (wc *WorkspaceController) DownloadWorkspace(c *gin.Context) {
	root := config.WorkingDir()
	if _, err := os.Stat(root); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "working dir does not exist"})
		return
	}
	data, err := zipDir(root)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	filename := "copaw_workspace_" + time.Now().UTC().Format("20060102_150405") + ".zip"
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Data(http.StatusOK, "application/zip", data)
}

func (wc *WorkspaceController) UploadWorkspace(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	src, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer src.Close()
	payload, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid zip archive"})
		return
	}
	root := config.WorkingDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for _, f := range zr.File {
		name := filepath.ToSlash(strings.TrimSpace(f.Name))
		if name == "" {
			continue
		}
		if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "zip contains unsafe path: " + name})
			return
		}
		dst := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		rc, err := f.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
