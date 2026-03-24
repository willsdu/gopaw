package routers

// 本文件实现 /api/envs/*：工作区 .env 键值对的列出、整表覆盖保存与单键删除（KEY=VALUE 行格式）。

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"gopaw/internal/config"
)

type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type EnvsController struct{}

func envFilePath() string {
	return filepath.Join(config.WorkingDir(), ".env")
}

func loadEnvs() (map[string]string, error) {
	out := map[string]string{}
	path := envFilePath()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, sc.Err()
}

func saveEnvs(envs map[string]string) error {
	if err := os.MkdirAll(config.WorkingDir(), 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(envs))
	for k := range envs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var lines []string
	for _, k := range keys {
		lines = append(lines, k+"="+envs[k])
	}
	return os.WriteFile(envFilePath(), []byte(strings.Join(lines, "\n")), 0o644)
}

func toEnvVarList(m map[string]string) []EnvVar {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]EnvVar, 0, len(keys))
	for _, k := range keys {
		out = append(out, EnvVar{Key: k, Value: m[k]})
	}
	return out
}

func (ec *EnvsController) ListEnvs(c *gin.Context) {
	envs, err := loadEnvs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toEnvVarList(envs))
}

func (ec *EnvsController) BatchSaveEnvs(c *gin.Context) {
	var body map[string]string
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	clean := map[string]string{}
	for k, v := range body {
		kk := strings.TrimSpace(k)
		if kk == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Key cannot be empty"})
			return
		}
		clean[kk] = v
	}
	if err := saveEnvs(clean); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toEnvVarList(clean))
}

func (ec *EnvsController) DeleteEnv(c *gin.Context) {
	key := c.Param("key")
	envs, err := loadEnvs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, ok := envs[key]; !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Env var '" + key + "' not found"})
		return
	}
	delete(envs, key)
	if err := saveEnvs(envs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toEnvVarList(envs))
}
