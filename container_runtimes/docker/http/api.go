package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/google/go-querystring/query"
	"github.com/metrue/fx/types"
	"github.com/metrue/fx/utils"
)

// API interact with dockerd http api
type API struct {
	endpoint string
	version  string
}

// Create a API
func Create(host string, port string) (*API, error) {
	version, err := utils.DockerVersion(host, port)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("http://%s:%s/v%s", host, port, version)
	return &API{
		endpoint: endpoint,
		version:  version,
	}, nil
}

// MustCreate a api object, panic if not
func MustCreate(host string, port string) *API {
	version, err := utils.DockerVersion(host, port)
	if err != nil {
		panic(err)
	}
	endpoint := fmt.Sprintf("http://%s:%s/v%s", host, port, version)
	return &API{
		endpoint: endpoint,
		version:  version,
	}
}

func (api *API) get(path string, qs string, v interface{}) error {
	url := fmt.Sprintf("%s%s", api.endpoint, path)
	if !strings.HasPrefix(url, "http") {
		url = "http://" + url
	}
	if qs != "" {
		url += "?" + qs
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("request %s failed: %d - %s", url, resp.StatusCode, resp.Status)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	err = json.Unmarshal(body, &v)
	if err != nil {
		return err
	}
	return nil
}

func (api *API) post(path string, body []byte, expectStatus int, v interface{}) error {
	url := fmt.Sprintf("%s%s", api.endpoint, path)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != expectStatus {
		return fmt.Errorf("request %s (%s) failed: %d - %s", url, string(body), resp.StatusCode, resp.Status)
	}

	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	err = json.Unmarshal(b, &v)
	if err != nil {
		return err
	}
	return nil
}

// List list service
func (api *API) list(name string) ([]types.Service, error) {
	if name != "" {
		info, err := api.inspect(name)
		if err != nil {
			return []types.Service{}, err
		}

		port, err := strconv.Atoi(info.HostConfig.PortBindings["3000/tcp"][0].HostPort)
		if err != nil {
			return []types.Service{}, err
		}
		return []types.Service{
			types.Service{
				Name:  name,
				Image: info.Image,
				State: info.State.Status,
				ID:    info.ID,
				Host:  info.HostConfig.PortBindings["3000/tcp"][0].HostIP,
				Port:  port,
			},
		}, nil
	}

	type filterItem struct {
		Status []string `json:"url,omitempty"`
		Label  []string `json:"label,omitempty"`
		Name   []string `json:"name,omitempty"`
	}

	type Filters struct {
		Items string `url:"filters"`
	}

	filter := filterItem{
		// Status: []string{"running"},
		Label: []string{"belong-to=fx"},
	}

	q, err := json.Marshal(filter)
	if err != nil {
		return []types.Service{}, err
	}

	filters := Filters{Items: string(q)}
	qs, err := query.Values(filters)
	if err != nil {
		return []types.Service{}, err
	}

	var containers []dockerTypes.Container
	if err := api.get("/containers/json", qs.Encode(), &containers); err != nil {
		return []types.Service{}, err
	}

	svs := make(map[string]types.Service)
	for _, container := range containers {
		// container name have extra forward slash
		// https://github.com/moby/moby/issues/6705
		if strings.HasPrefix(container.Names[0], fmt.Sprintf("/%s", name)) {
			svs[container.Image] = types.Service{
				Name:  container.Names[0],
				Image: container.Image,
				ID:    container.ID,
				Host:  container.Ports[0].IP,
				Port:  int(container.Ports[0].PublicPort),
				State: container.State,
			}
		}
	}
	services := []types.Service{}
	for _, s := range svs {
		services = append(services, s)
	}

	return services, nil
}
