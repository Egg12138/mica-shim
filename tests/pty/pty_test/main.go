package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"syscall"
	"time"
)

// DummyShell 模拟一个简单的shell
type DummyShell struct {
	input  io.Reader
	output io.Writer
}

// NewDummyShell 创建一个新的DummyShell实例
func NewDummyShell(input io.Reader, output io.Writer) *DummyShell {
	return &DummyShell{
		input:  input,
		output: output,
	}
}

// Run 运行DummyShell
func (ds *DummyShell) Run() {
	// 在单独的goroutine中处理输出，避免阻塞
	outputChan := make(chan string, 10)
	go func() {
		for output := range outputChan {
			ds.output.Write([]byte(output))
		}
	}()

	// 显示欢迎信息
	outputChan <- "Dummy shell started. Type 'help' for available commands, 'exit' to quit.\n$ "

	scanner := bufio.NewScanner(ds.input)

	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())

		switch input {
		case "exit":
			outputChan <- "Exiting dummy shell\n"
			close(outputChan)
			return
		case "help":
			outputChan <- "Available commands: help, echo, date, exit\n$ "
		case "date":
			outputChan <- fmt.Sprintf("Current time: %s\n$ ", time.Now().Format("2006-01-02 15:04:05"))
		case "":
			// 空命令，只显示提示符
			outputChan <- "$ "
		default:
			if strings.HasPrefix(input, "echo ") {
				outputChan <- strings.TrimPrefix(input, "echo ") + "\n$ "
			} else {
				outputChan <- fmt.Sprintf("Unknown command: %s. Type 'help' for available commands.\n$ ", input)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading input: %v", err)
	}

	close(outputChan)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  go run pty_test.go dummy    # Create dummy shell with FIFO")
		fmt.Println("  go run pty_test.go test     # Test with stdin/stdout")
		return
	}

	switch os.Args[1] {
	case "dummy":
		runDummyShellWithFIFO()
	case "test":
		runDummyShellWithStdIO()
	default:
		fmt.Printf("Unknown option: %s\n", os.Args[1])
	}
}

func runDummyShellWithFIFO() {
	// 创建命名管道（FIFO）
	fifoPath := "/tmp/ttyDummy"

	// 删除可能存在的旧FIFO
	if err := os.Remove(fifoPath); err != nil && !os.IsNotExist(err) {
		log.Printf("Note: Failed to remove existing FIFO: %v", err)
	}

	// 创建新的FIFO
	if err := syscall.Mkfifo(fifoPath, 0666); err != nil {
		log.Fatalf("Failed to create FIFO: %v", err)
	}

	fmt.Printf("Created FIFO at %s\n", fifoPath)
	fmt.Printf("Test with:\n")
	fmt.Printf("  echo 'help' > %s\n", fifoPath)
	fmt.Printf("  echo 'date' > %s\n", fifoPath)
	fmt.Printf("  echo 'echo Hello World' > %s\n", fifoPath)
	fmt.Printf("View output with:\n")
	fmt.Printf("  cat %s\n", fifoPath)
	fmt.Printf("Press Ctrl+C to exit\n")

	// 以读写模式打开FIFO，避免阻塞
	fifo, err := os.OpenFile(fifoPath, os.O_RDWR, 0666)
	if err != nil {
		log.Fatalf("Failed to open FIFO: %v", err)
	}
	defer fifo.Close()

	// 创建DummyShell实例
	dummyShell := NewDummyShell(fifo, fifo)

	// 运行DummyShell
	dummyShell.Run()
}

func runDummyShellWithStdIO() {
	fmt.Println("Running dummy shell with stdin/stdout")
	fmt.Println("Type 'help' for available commands, 'exit' to quit.")

	// 创建DummyShell实例，使用标准输入输出
	dummyShell := NewDummyShell(os.Stdin, os.Stdout)

	// 运行DummyShell
	dummyShell.Run()

	fmt.Println("\nDummy shell exited")
}
