CC = gcc
CFLAGS = -Wall -Wextra -g
LDFLAGS = 

BUILD_DIRS := ../../builds/
TARGET = mock_micad
SRCS = mock_micad.c
OBJS = $(SRCS:.c=.o)

.PHONY: all clean simple

all: $(TARGET)

simple: CFLAGS += -DSIMPLE_MODE
simple: $(OBJS)
	$(CC) $(OBJS) -o simple-$(TARGET) $(LDFLAGS)

$(TARGET): $(OBJS)
	$(CC) $(OBJS) -o $(BUILD_DIRS)$(TARGET) $(LDFLAGS)

%.o: %.c
	$(CC) $(CFLAGS) -c $< -o $@

clean:
	rm -f $(OBJS) $(TARGET) 
