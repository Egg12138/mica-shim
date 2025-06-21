package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/containerd/remotes/docker"
)

const imageName = "docker.io/library/busybox:latest"
const containerName = "busybox-test"

func main() {
	if err := cntrExample(); err != nil {
		log.Fatal(err)
	}
}

func setupProxyTransport() *http.Transport {
	transport := &http.Transport{}
	
	if httpProxy := os.Getenv("HTTP_PROXY"); httpProxy != "" {
		proxyURL, err := url.Parse(httpProxy)
		if err != nil {
			log.Printf("Warning: Invalid HTTP_PROXY URL: %v", err)
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
			log.Printf("Using HTTP proxy: %s", httpProxy)
		}
	}
	
	if httpsProxy := os.Getenv("HTTPS_PROXY"); httpsProxy != "" {
		if transport.Proxy == nil {
			proxyURL, err := url.Parse(httpsProxy)
			if err != nil {
				log.Printf("Warning: Invalid HTTPS_PROXY URL: %v", err)
			} else {
				transport.Proxy = http.ProxyURL(proxyURL)
				log.Printf("Using HTTPS proxy: %s", httpsProxy)
			}
		}
	}
	
	return transport
}

// listExistingImages 列出所有现有镜像
func listExistingImages(client *containerd.Client, ctx context.Context) error {
	log.Println("Listing all existing images...")
	images, err := client.ListImages(ctx)
	if err != nil {
		return fmt.Errorf("failed to list images: %w", err)
	}

	if len(images) == 0 {
		log.Println("No images found in containerd")
		return nil
	}

	log.Printf("Found %d existing images:", len(images))
	for i, img := range images {
		// Get image size
		size, err := img.Size(ctx)
		if err != nil {
			log.Printf("  %d. %s (size: unknown)", i+1, img.Name())
		} else {
			log.Printf("  %d. %s (size: %.2f MB)", i+1, img.Name(), float64(size)/(1024*1024))
		}
	}
	log.Println("") // Empty line for better readability

	return nil
}

func cntrExample() error {
	// create a new client connected to the default socket path for containerd
	client, err := containerd.New(
		"/run/containerd/containerd.sock",
		containerd.WithDefaultNamespace("moby"))
	if err != nil {
		return err
	}
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "example")

	transport := setupProxyTransport()
	
	resolver := docker.NewResolver(docker.ResolverOptions{
		Hosts: docker.ConfigureDefaultRegistries(
			docker.WithClient(&http.Client{
				Transport: transport,
				Timeout:   30 * time.Second,
			}),
		),
	})

	// List all existing images first
	if err := listExistingImages(client, ctx); err != nil {
		return fmt.Errorf("failed to list existing images: %w", err)
	}

	log.Printf("Checking if image %s already exists...", imageName)
	image, err := client.GetImage(ctx, imageName)
	if err != nil {
		log.Printf("Image %s not found locally, pulling...", imageName)
		image, err = client.Pull(ctx, imageName, 
			containerd.WithPullUnpack,
			containerd.WithResolver(resolver),
		)
		if err != nil {
			return fmt.Errorf("failed to pull image: %w", err)
		}
		log.Println("Image pulled successfully")
		
		// List images again to show the newly pulled image
		log.Println("Updated image list after pulling:")
		if err := listExistingImages(client, ctx); err != nil {
			log.Printf("Warning: failed to list images after pull: %v", err)
		}
	} else {
		log.Printf("Image %s already exists locally, skipping pull", imageName)
	}

	// create a container
	log.Println("Creating container...")
	container, err := client.NewContainer(
		ctx,
		containerName,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(containerName+"-snapshot", image),
		containerd.WithNewSpec(oci.WithImageConfig(image)),
	)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}
	defer container.Delete(ctx, containerd.WithSnapshotCleanup)

	// create a task from the container
	log.Println("Creating task...")
	task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStdio))
	if err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}
	defer task.Delete(ctx)

	// make sure we wait before calling start
	exitStatusC, err := task.Wait(ctx)
	if err != nil {
		return fmt.Errorf("failed to wait for task: %w", err)
	}

	// call start on the task to execute the busybox container
	log.Printf("Starting %s container...", containerName)
	if err := task.Start(ctx); err != nil {
		return fmt.Errorf("failed to start task: %w", err)
	}

	// sleep for a lil bit to see the logs
	log.Printf("%s container is running, waiting 3 seconds...\n", containerName)
	time.Sleep(3 * time.Second)

	// kill the process and get the exit status
	log.Printf("Stopping %s container...", containerName)
	if err := task.Kill(ctx, syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to kill task: %w", err)
	}

	// wait for the process to fully exit and print out the exit status
	status := <-exitStatusC
	code, _, err := status.Result()
	if err != nil {
		return fmt.Errorf("failed to get exit status: %w", err)
	}
	fmt.Printf("%s container exited with status: %d\n", containerName, code)

	return nil
}