package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

type logger interface {
	Printf(string, ...interface{})
}

type nopLogger struct{}

func (nopLogger) Printf(string, ...interface{}) {}

type jsonLogger struct {
	level string
}

func (l jsonLogger) Printf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	m := map[string]string{
		"level": l.level,
		"time":  time.Now().Format(time.RFC3339),
		"msg":   msg,
	}
	b, _ := json.Marshal(m)
	fmt.Println(string(b))
}

var (
	infoLog  logger
	warnLog  logger
	debugLog logger
)

func initLoggers(level, format string) {
	lvl := strings.ToLower(level)
	fmtType := strings.ToLower(format)
	switch fmtType {
	case "json":
		infoLog = jsonLogger{level: "info"}
		warnLog = jsonLogger{level: "warn"}
		debugLog = jsonLogger{level: "debug"}
	default:
		infoLog = log.New(os.Stdout, "INFO: ", log.LstdFlags)
		warnLog = log.New(os.Stdout, "WARNING: ", log.LstdFlags)
		debugLog = log.New(os.Stdout, "DEBUG: ", log.LstdFlags)
	}
	switch lvl {
	case "debug":
	case "info":
		debugLog = nopLogger{}
	case "warn", "warning":
		infoLog = nopLogger{}
		debugLog = nopLogger{}
	default:
		debugLog = nopLogger{}
	}
}
