package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
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
	scanner := bufio.NewScanner(ds.input)

	ds.output.Write([]byte("Dummy shell started. Type 'help' for available commands, 'exit' to quit.\n"))
	ds.output.Write([]byte("$ "))

	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())

		switch input {
		case "exit":
			ds.output.Write([]byte("Exiting dummy shell\n"))
			return
		case "help":
			ds.output.Write([]byte("Available commands: help, echo, exit\n"))
		case "":
			// 空命令，显示提示符
			ds.output.Write([]byte("$ "))
			continue
		default:
			if strings.HasPrefix(input, "echo ") {
				ds.output.Write([]byte(strings.TrimPrefix(input, "echo ") + "\n"))
			} else {
				ds.output.Write([]byte(fmt.Sprintf("Unknown command: %s. Type 'help' for available commands.\n", input)))
			}
		}

		ds.output.Write([]byte("$ "))
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading input: %v", err)
	}
}

func main() {
	// 创建一个管道用于模拟终端通信
	r, w, err := os.Pipe()
	if err != nil {
		log.Fatalf("Failed to create pipe: %v", err)
	}

	// 创建DummyShell实例，使用管道进行通信
	dummyShell := NewDummyShell(r, os.Stdout)

	// 在goroutine中运行DummyShell
	go func() {
		dummyShell.Run()
	}()

	// 从标准输入读取并写入到DummyShell
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		input := scanner.Text()
		// 将输入写入管道
		if _, err := w.Write([]byte(input + "\n")); err != nil {
			log.Printf("Error writing to pipe: %v", err)
			break
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading standard input: %v", err)
	}
}
