package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/apex/log"
	"github.com/google/go-querystring/query"
	"github.com/google/uuid"
	"github.com/metrue/fx/types"
	"github.com/metrue/fx/utils"
)

func makeTar(project types.Project, tarFilePath string) error {
	dir, err := ioutil.TempDir("/tmp", "fx-build-dir")
	if err != nil {
		return err
	}

	defer os.RemoveAll(dir)

	for _, file := range project.Files {
		tmpfn := filepath.Join(dir, file.Path)
		if err := utils.EnsureFile(tmpfn); err != nil {
			return err
		}
		if err := ioutil.WriteFile(tmpfn, []byte(file.Body), 0666); err != nil {
			return err
		}
	}

	return utils.TarDir(dir, tarFilePath)
}

// Build build a project
func (api *API) Build(project types.Project) (types.Service, error) {
	tarDir, err := ioutil.TempDir("/tmp", "fx-tar")
	if err != nil {
		return types.Service{}, err
	}
	defer os.RemoveAll(tarDir)

	imageID := uuid.New().String()
	tarFilePath := filepath.Join(tarDir, fmt.Sprintf("%s.tar", imageID))
	if err := makeTar(project, tarFilePath); err != nil {
		return types.Service{}, err
	}
	labels := map[string]string{
		"belong-to": "fx",
	}
	if err := api.BuildImage(tarFilePath, imageID, labels); err != nil {
		return types.Service{}, err
	}

	return types.Service{
		Name:  project.Name,
		Image: imageID,
	}, nil
}

// BuildImage build docker image
func (api *API) BuildImage(tarFile string, tag string, labels map[string]string) error {
	dockerBuildContext, err := os.Open(tarFile)
	if err != nil {
		return err
	}
	defer dockerBuildContext.Close()

	type buildQuery struct {
		Labels     string `url:"labels,omitempty"`
		Tags       string `url:"t,omitempty"`
		Dockerfile string `url:"dockerfile,omitempty"`
	}

	// Apply default labels
	labelsJSON, _ := json.Marshal(labels)
	q := buildQuery{
		Tags:       tag,
		Labels:     string(labelsJSON),
		Dockerfile: "Dockerfile",
	}
	qs, err := query.Values(q)
	if err != nil {
		return err
	}

	path := "/build"
	url := fmt.Sprintf("%s%s?%s", api.endpoint, path, qs.Encode())
	req, err := http.NewRequest("POST", url, dockerBuildContext)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-tar")
	client := &http.Client{Timeout: 600 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)

	for scanner.Scan() {
		if os.Getenv("DEBUG") != "" {
			log.Infof(scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
