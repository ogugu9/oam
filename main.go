package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"golang.org/x/sync/semaphore"
	"gopkg.in/yaml.v2"
)

type Repo struct {
	URL     string `yaml:"url"`
	Version string `yaml:"version"`
	Path    string `yaml:"path"`
}

type Config struct {
	OutputDir string          `yaml:"output_dir"`
	Repos     map[string]Repo `yaml:"repos"`
}

var (
	wg    sync.WaitGroup // WaitGroup to wait for all goroutines to finish.
	cache sync.Map       // Cache to store and retrieve OpenAPI files.
)

func fetchFile(sema *semaphore.Weighted, repoName string, r Repo, outputDir string) {
	defer wg.Done()       // Notify WaitGroup that this goroutine is done.
	defer sema.Release(1) // Release a spot in the semaphore.

	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", r.URL, r.Version, r.Path)

	// Check if the data is already in cache.
	if v, ok := cache.Load(url); ok {
		writeFile(repoName, r, outputDir, v.([]byte))
		return
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	// If private repository, set necessary headers for authentication with GitHub token.
	if username, token := os.Getenv("GITHUB_USERNAME"), os.Getenv("GITHUB_TOKEN"); username != "" && token != "" {
		req.SetBasicAuth(username, token)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		fmt.Printf("Failed to fetch %s: %s\n", url, res.Status)
		return
	}

	fileData, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Save the file data to the cache.
	cache.Store(url, fileData)

	writeFile(repoName, r, outputDir, fileData)
}

func writeFile(repoName string, r Repo, outputDir string, data []byte) {
	destDir := fmt.Sprintf("%s/%s", outputDir, repoName)
	err := os.MkdirAll(destDir, 0755)
	if err != nil {
		fmt.Println(err)
		return
	}

	destFile := fmt.Sprintf("%s/%s.yaml", destDir, repoName)
	err = os.WriteFile(destFile, data, 0644)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("Saved %s\n", destFile)
}

func main() {
	data, err := os.ReadFile("oam.yaml")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	sema := semaphore.NewWeighted(20) // Semaphore to rate limit API calls.
	for repoName, r := range config.Repos {
		err := sema.Acquire(context.Background(), 1) // Grab a spot in the semaphore.
		if err != nil {
			fmt.Println(err)
			continue
		}

		wg.Add(1) // Notify the WaitGroup that a new goroutine is starting.
		go fetchFile(sema, repoName, r, config.OutputDir)
	}

	wg.Wait() // Wait for all goroutines to finish.
}
