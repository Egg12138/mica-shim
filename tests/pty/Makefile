.PHONY: all clean pty-test fifo-test dummy-shell

all: pty-test fifo-test stdio-test

pty-test:
	go run pty_experiment.go pty

fifo-test:
	go run pty_experiment.go fifo

stdio-test:
	go run pty_experiment.go stdio

dummy-shell:
	go run dummy_shell.go
