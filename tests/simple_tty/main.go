package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	dev := flag.String("dev", "/dev/ttyRPMSG0", "device to connect to")
	flag.Parse()

	fmt.Printf("Opening device: %s\n", *dev)

	f, err := os.OpenFile(*dev, os.O_RDWR, 0)
	if err != nil {
		log.Fatalf("Failed to open device %s: %v", *dev, err)
	}
	defer f.Close()

	fmt.Println("Device opened. Forwarding IO. Press Ctrl+C to exit.")

	// Channel to signal goroutines to stop
	done := make(chan struct{})

	// Goroutine to read from device and write to stdout
	go func() {
		_, err := io.Copy(os.Stdout, f)
		select {
		case <-done:
			// Expected when closing
		default:
			if err != nil {
				log.Printf("Error reading from device: %v", err)
			}
		}
		fmt.Println("Device->Stdout forwarder stopped.")
	}()

	// Goroutine to read from stdin and write to device
	go func() {
		_, err := io.Copy(f, os.Stdin)
		select {
		case <-done:
			// Expected when closing
		default:
			if err != nil {
				log.Printf("Error writing to device: %v", err)
			}
		}
		fmt.Println("Stdin->Device forwarder stopped.")
	}()

	// Wait for a signal to gracefully shut down
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nReceived signal, shutting down...")
	close(done) // Signal goroutines to stop
}
