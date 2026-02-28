package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Logger struct {
	moduleName string
	pathName string
}

func New(module string, path string) *Logger {
	return &Logger{moduleName: module, pathName: path}
}

func (l *Logger) log(level, message string) {
	now := time.Now()
	timestamp := now.Format("15:04:05") 
	dateFile := now.Format("02-01-2006") 

	logDir := filepath.Join(l.pathName, "logs")
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		os.MkdirAll(logDir, 0755)
	}

	logEntry := fmt.Sprintf("[%s]-[%s]-[%s]-%s\n", timestamp, level, l.moduleName, message)

	filePath := filepath.Join(logDir, dateFile+".txt")
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Logging Error: %v\n", err)
		return
	}
	defer f.Close()

	f.WriteString(logEntry)
	fmt.Print(logEntry)
}

func (l *Logger) Info(msg string)  { l.log("INFO", msg) }
func (l *Logger) Debug(msg string) { l.log("DEBUG", msg) }
func (l *Logger) Error(msg string) { l.log("ERROR", msg) }