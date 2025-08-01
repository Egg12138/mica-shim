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

const (
	defaultImageName     = "docker.io/library/busybox:latest"
	defaultContainerName = "busybox-test"
	defaultNamespace     = "default"
	containerdSocketPath = "/run/containerd/containerd.sock"
	mobyNamespace        = "moby"
	clientTestLabel      = "client-test"
	waitTimeout          = 3 * time.Second
	httpTimeout          = 30 * time.Second
)

var customRuntimeName string

func main() {
	if err := runContainerdExample(); err != nil {
		log.Fatal(err)
	}
}

func configureProxyTransport() *http.Transport {
	transport := &http.Transport{}

	if httpProxy := os.Getenv("HTTP_PROXY"); httpProxy != "" {
		if proxyURL, err := url.Parse(httpProxy); err != nil {
			log.Printf("Warning: Invalid HTTP_PROXY URL: %v", err)
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
			log.Printf("Using HTTP proxy: %s", httpProxy)
		}
	}

	if httpsProxy := os.Getenv("HTTPS_PROXY"); httpsProxy != "" {
		if transport.Proxy == nil {
			if proxyURL, err := url.Parse(httpsProxy); err != nil {
				log.Printf("Warning: Invalid HTTPS_PROXY URL: %v", err)
			} else {
				transport.Proxy = http.ProxyURL(proxyURL)
				log.Printf("Using HTTPS proxy: %s", httpsProxy)
			}
		}
	}

	return transport
}

func displayExistingImages(client *containerd.Client, ctx context.Context) error {
	log.Println("Listing all existing images in namespace", defaultNamespace)
	images, err := client.ListImages(ctx)
	if err != nil {
		return fmt.Errorf("failed to list images: %w", err)
	}

	if len(images) == 0 {
		log.Println("No images found in containerd")
		return nil
	}

	log.Printf("Found %d existing images:", len(images))
	for index, currentImage := range images {
		imageSize, err := currentImage.Size(ctx)
		if err != nil {
			log.Printf("  %d. %s (size: unknown)", index+1, currentImage.Name())
		} else {
			sizeMB := float64(imageSize) / (1024 * 1024)
			log.Printf("  %d. %s (size: %.2f MB)", index+1, currentImage.Name(), sizeMB)
		}
	}
	log.Println("")

	return nil
}

func cleanupExistingTestContainers(client *containerd.Client, ctx context.Context) error {
	existingContainers, err := client.ContainerService().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	for _, existingContainer := range existingContainers {
		if existingContainer.Labels[clientTestLabel] == "true" {
			log.Printf("Deleting existing test container %s", existingContainer.ID)
			log.Printf(" CreatedAt: %v", existingContainer.CreatedAt)
			log.Printf(" Image: %v", existingContainer.Image)
			log.Printf(" Labels: %v", existingContainer.Labels)

			if err := client.ContainerService().Delete(ctx, existingContainer.ID); err != nil {
				return fmt.Errorf("failed to delete container: %w", err)
			}
		}
	}
	return nil
}

func buildContainerOptions(containerImage containerd.Image) []containerd.NewContainerOpts {
	snapshotName := defaultContainerName + "-snapshot"

	containerOptions := []containerd.NewContainerOpts{
		containerd.WithImage(containerImage),
		containerd.WithNewSnapshot(snapshotName, containerImage),
		containerd.WithNewSpec(oci.WithImageConfig(containerImage)),
		containerd.WithContainerLabels(map[string]string{
			clientTestLabel: "true",
		}),
	}

	if customRuntimeName != "" {
		containerOptions = append(containerOptions, containerd.WithRuntime(customRuntimeName, nil))
	}

	return containerOptions
}

func pullImageIfNeeded(client *containerd.Client, ctx context.Context, imageName string, resolver docker.ResolverOptions) (containerd.Image, error) {
	log.Printf("Checking if image %s already exists...", imageName)

	containerImage, err := client.GetImage(ctx, imageName)
	if err != nil {
		log.Printf("Image %s not found locally, pulling...", imageName)

		containerImage, err = client.Pull(ctx, imageName,
			containerd.WithPullUnpack,
			containerd.WithResolver(docker.NewResolver(resolver)),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to pull image: %w", err)
		}

		log.Println("Image pulled successfully")
		log.Println("Updated image list after pulling:")
		if err := displayExistingImages(client, ctx); err != nil {
			log.Printf("Warning: failed to list images after pull: %v", err)
		}
	} else {
		log.Printf("Image %s already exists locally, skipping pull", imageName)
	}

	return containerImage, nil
}

func createAndRunContainer(client *containerd.Client, ctx context.Context, containerImage containerd.Image) error {
	log.Println("Creating container...")

	if customRuntimeName != "" {
		log.Printf("Using custom runtime: %s", customRuntimeName)
	} else {
		log.Println("Using default runtime")
	}

	if err := cleanupExistingTestContainers(client, ctx); err != nil {
		return err
	}

	containerOptions := buildContainerOptions(containerImage)
	newContainer, err := client.NewContainer(ctx, defaultContainerName, containerOptions...)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}
	defer newContainer.Delete(ctx, containerd.WithSnapshotCleanup)

	log.Println("Creating task...")
	containerTask, err := newContainer.NewTask(ctx, cio.NewCreator(cio.WithStdio))
	if err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}
	defer containerTask.Delete(ctx)

	exitStatusChannel, err := containerTask.Wait(ctx)
	if err != nil {
		return fmt.Errorf("failed to wait for task: %w", err)
	}

	log.Printf("Starting %s container...", defaultContainerName)
	if err := containerTask.Start(ctx); err != nil {
		return fmt.Errorf("failed to start task: %w", err)
	}

	log.Printf("%s container is running, waiting %v...\n", defaultContainerName, waitTimeout)
	time.Sleep(waitTimeout)

	log.Printf("Stopping %s container...", defaultContainerName)
	if err := containerTask.Kill(ctx, syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to kill task: %w", err)
	}

	taskStatus := <-exitStatusChannel
	exitCode, _, err := taskStatus.Result()
	if err != nil {
		return fmt.Errorf("failed to get exit status: %w", err)
	}

	log.Printf("%s container exited with status: %d\n", defaultContainerName, exitCode)
	return nil
}

func runContainerdExample() error {
	containerdClient, err := containerd.New(
		containerdSocketPath,
		containerd.WithDefaultNamespace(mobyNamespace))
	if err != nil {
		return err
	}
	defer containerdClient.Close()

	executionContext := namespaces.WithNamespace(context.Background(), defaultNamespace)
	httpTransport := configureProxyTransport()

	imageResolver := docker.ResolverOptions{
		Hosts: docker.ConfigureDefaultRegistries(
			docker.WithClient(&http.Client{
				Transport: httpTransport,
				Timeout:   httpTimeout,
			}),
		),
	}

	if err := displayExistingImages(containerdClient, executionContext); err != nil {
		return fmt.Errorf("failed to list existing images: %w", err)
	}

	containerImage, err := pullImageIfNeeded(containerdClient, executionContext, defaultImageName, imageResolver)
	if err != nil {
		return err
	}

	return createAndRunContainer(containerdClient, executionContext, containerImage)
}
