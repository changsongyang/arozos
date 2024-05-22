package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

/*
	ArozOS System Logger

	This script is designed to make a managed log for the ArozOS system
	and replace the ton of log.Println in the system core
*/

type Logger struct {
	LogToFile      bool     //Set enable write to file
	Prefix         string   //Prefix for log files
	LogFolder      string   //Folder to store the log  file
	CurrentLogFile string   //Current writing filename
	file           *os.File //File, empty if LogToFile is false
}

// Create a default logger
func NewLogger(logFilePrefix string, logFolder string, logToFile bool) (*Logger, error) {
	if logToFile {
		err := os.MkdirAll(logFolder, 0775)
		if err != nil {
			return nil, err
		}
	}

	thisLogger := Logger{
		LogToFile: logToFile,
		Prefix:    logFilePrefix,
		LogFolder: logFolder,
	}

	if logToFile {
		logFilePath := thisLogger.getLogFilepath()
		f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0755)
		if err != nil {
			return nil, err
		}
		thisLogger.CurrentLogFile = logFilePath
		thisLogger.file = f
	}

	return &thisLogger, nil
}

// Create a non-persistent logger for one-time uses
func NewTmpLogger() (*Logger, error) {
	return NewLogger("", "", false)
}

func (l *Logger) getLogFilepath() string {
	year, month, _ := time.Now().Date()
	return filepath.Join(l.LogFolder, l.Prefix+"_"+strconv.Itoa(year)+"-"+strconv.Itoa(int(month))+".log")
}

// PrintAndLog will log the message to file and print the log to STDOUT
func (l *Logger) PrintAndLog(title string, message string, originalError error) {
	go func() {
		l.Log(title, message, originalError)
	}()
	log.Println("[" + title + "] " + message)
}

func (l *Logger) Log(title string, errorMessage string, originalError error) {
	if l.LogToFile {
		l.ValidateAndUpdateLogFilepath()
		if originalError == nil {
			l.file.WriteString(time.Now().Format("2006-01-02 15:04:05.000000") + "|" + fmt.Sprintf("%-16s", title) + " [INFO]" + errorMessage + "\n")
		} else {
			l.file.WriteString(time.Now().Format("2006-01-02 15:04:05.000000") + "|" + fmt.Sprintf("%-16s", title) + " [ERROR]" + errorMessage + " " + originalError.Error() + "\n")
		}
	}

}

// Validate if the logging target is still valid (detect any months change)
func (l *Logger) ValidateAndUpdateLogFilepath() {
	expectedCurrentLogFilepath := l.getLogFilepath()
	if l.CurrentLogFile != expectedCurrentLogFilepath {
		//Change of month. Update to a new log file
		l.file.Close()
		f, err := os.OpenFile(expectedCurrentLogFilepath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0755)
		if err != nil {
			log.Println("[Logger] Unable to create new log. Logging to file disabled.")
			l.LogToFile = false
			return
		}
		l.CurrentLogFile = expectedCurrentLogFilepath
		l.file = f
	}
}

func (l *Logger) Close() {
	l.file.Close()
}
