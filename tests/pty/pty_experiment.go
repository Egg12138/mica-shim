package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// DummyPTY 模拟PTY主设备
type DummyPTY struct {
	masterRead  *os.File
	masterWrite *os.File
	slaveRead   *os.File
	slaveWrite  *os.File
}

// NewDummyPTY 创建一个新的虚拟PTY
func NewDummyPTY() (*DummyPTY, error) {
	// 创建两个管道来模拟PTY的主从设备
	// 主设备读取 <-> 从设备写入
	masterRead, slaveWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	// 从设备读取 <-> 主设备写入
	slaveRead, masterWrite, err := os.Pipe()
	if err != nil {
		masterRead.Close()
		slaveWrite.Close()
		return nil, err
	}

	return &DummyPTY{
		masterRead:  masterRead,
		masterWrite: masterWrite,
		slaveRead:   slaveRead,
		slaveWrite:  slaveWrite,
	}, nil
}

// Close 关闭所有PTY文件描述符
func (pty *DummyPTY) Close() {
	pty.masterRead.Close()
	pty.masterWrite.Close()
	pty.slaveRead.Close()
	pty.slaveWrite.Close()
}

// Master 获取主设备端（用于程序端）
func (pty *DummyPTY) Master() (io.Reader, io.Writer) {
	return pty.masterRead, pty.masterWrite
}

// Slave 获取从设备端（用于shell端）
func (pty *DummyPTY) Slave() (io.Reader, io.Writer) {
	return pty.slaveRead, pty.slaveWrite
}

// DummyShell 模拟shell
type DummyShell struct {
	reader   io.Reader
	writer   io.Writer
	quitChan chan struct{}
}

// NewDummyShell 创建一个新的虚拟shell
func NewDummyShell(reader io.Reader, writer io.Writer) *DummyShell {
	return &DummyShell{
		reader:   reader,
		writer:   writer,
		quitChan: make(chan struct{}),
	}
}

// Run 运行虚拟shell
func (ds *DummyShell) Run() {
	fmt.Fprintf(ds.writer, "Dummy shell started. Type 'help' for available commands, 'exit' to quit.\n")
	fmt.Fprintf(ds.writer, "$ ")

	scanner := bufio.NewScanner(ds.reader)
	for {
		select {
		case <-ds.quitChan:
			fmt.Fprintf(ds.writer, "\nShell terminated\n")
			return
		default:
			if !scanner.Scan() {
				// 输入流结束
				return
			}

			input := strings.TrimSpace(scanner.Text())

			switch input {
			case "exit":
				fmt.Fprintf(ds.writer, "Exiting dummy shell\n")
				return
			case "help":
				fmt.Fprintf(ds.writer, "Available commands: help, echo, date, exit\n")
			case "date":
				fmt.Fprintf(ds.writer, "Current time: %s\n", time.Now().Format("2006-01-02 15:04:05"))
			case "":
				// 空命令
			default:
				if strings.HasPrefix(input, "echo ") {
					fmt.Fprintf(ds.writer, "%s\n", strings.TrimPrefix(input, "echo "))
				} else {
					fmt.Fprintf(ds.writer, "Unknown command: %s. Type 'help' for available commands.\n", input)
				}
			}

			fmt.Fprintf(ds.writer, "$ ")
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading input: %v", err)
	}
}

// Stop 停止shell
func (ds *DummyShell) Stop() {
	close(ds.quitChan)
}

// createFIFO 创建FIFO文件
func createFIFO(path string) error {
	// 删除可能存在的旧FIFO
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	// 创建新的FIFO
	return syscall.Mkfifo(path, 0666)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  go run pty_experiment.go pty     # Test with virtual PTY")
		fmt.Println("  go run pty_experiment.go fifo    # Test with FIFO")
		return
	}

	switch os.Args[1] {
	case "pty":
		testWithPTY()
	case "fifo":
		testWithFIFO()
	default:
		fmt.Printf("Unknown option: %s\n", os.Args[1])
	}
}

func testWithPTY() {
	fmt.Println("Testing with virtual PTY...")
	fmt.Println("Press Ctrl+C to exit")

	// 创建一个context用于处理取消信号
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// 在单独的goroutine中处理信号
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal, shutting down...")
		cancel()
	}()

	// 创建虚拟PTY
	pty, err := NewDummyPTY()
	if err != nil {
		log.Fatalf("Failed to create virtual PTY: %v", err)
	}
	defer pty.Close()

	// 获取主从设备端
	masterReader, masterWriter := pty.Master()
	slaveReader, slaveWriter := pty.Slave()

	// 启动虚拟shell（在从设备端）
	dummyShell := NewDummyShell(slaveReader, slaveWriter)

	// 在另一个goroutine中运行shell
	go func() {
		dummyShell.Run()
		cancel() // shell退出时取消context
	}()

	// 模拟用户输入（写入主设备）
	go func() {
		time.Sleep(100 * time.Millisecond) // 等待shell启动
		fmt.Fprintf(masterWriter, "help\n")
		time.Sleep(100 * time.Millisecond)
		fmt.Fprintf(masterWriter, "date\n")
		time.Sleep(100 * time.Millisecond)
		fmt.Fprintf(masterWriter, "echo Hello from PTY test!\n")
		time.Sleep(100 * time.Millisecond)
		fmt.Fprintf(masterWriter, "exit\n")
	}()

	// 读取并显示输出（从主设备读取）
	scanner := bufio.NewScanner(masterReader)
	for {
		select {
		case <-ctx.Done():
			fmt.Println("PTY test completed")
			return
		default:
			if scanner.Scan() {
				fmt.Printf("Output: %s\n", scanner.Text())
			} else {
				// 输入流结束
				return
			}
		}
	}
}

func testWithFIFO() {
	fifoPath := "/tmp/ttyDummy"

	fmt.Printf("Testing with FIFO at %s\n", fifoPath)
	fmt.Println("Press Ctrl+C to exit")

	// 创建一个context用于处理取消信号
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// 在单独的goroutine中处理信号
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal, shutting down...")
		cancel()
	}()

	// 创建FIFO
	if err := createFIFO(fifoPath); err != nil {
		log.Fatalf("Failed to create FIFO: %v", err)
	}

	// 以读写模式打开FIFO
	fifo, err := os.OpenFile(fifoPath, os.O_RDWR, 0666)
	if err != nil {
		log.Fatalf("Failed to open FIFO: %v", err)
	}
	defer fifo.Close()

	// 启动虚拟shell
	dummyShell := NewDummyShell(fifo, fifo)

	// 在另一个goroutine中运行shell
	go func() {
		dummyShell.Run()
		cancel() // shell退出时取消context
	}()

	// 发送测试命令
	go func() {
		time.Sleep(100 * time.Millisecond) // 等待shell启动
		fmt.Printf("Sending test commands...\n")
		fmt.Fprintf(fifo, "help\n")
		time.Sleep(100 * time.Millisecond)
		fmt.Fprintf(fifo, "date\n")
		time.Sleep(100 * time.Millisecond)
		fmt.Fprintf(fifo, "echo Hello from FIFO test!\n")
		time.Sleep(100 * time.Millisecond)
		fmt.Fprintf(fifo, "exit\n")
	}()

	// 等待完成或中断信号
	<-ctx.Done()

	fmt.Println("FIFO test completed")
	fmt.Printf("You can also test manually:\n")
	fmt.Printf("  echo 'help' > %s\n", fifoPath)
	fmt.Printf("  cat %s  # (in another terminal)\n", fifoPath)
}
