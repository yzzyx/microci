package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

var (
	errExecCancelled = errors.New("Execution cancelledby user")
	errExecTimedOut  = errors.New("Execution timed out")
)

// exportVar converts a struct to a list of variables to be exported to the shell
func exportVar(prefix string, i interface{}) []string {
	v := reflect.ValueOf(i)

	if v.Kind() != reflect.Struct {
		return nil
	}

	var variableList []string
	for fieldIdx := 0; fieldIdx < v.NumField(); fieldIdx++ {
		f := v.Field(fieldIdx)
		name := v.Type().Field(fieldIdx).Name
		r, _ := utf8.DecodeRuneInString(name)
		if unicode.IsLower(r) {
			continue
		}

		name = strings.ToUpper(name)
		if prefix != "" {
			name = prefix + "_" + name
		}
		if f.Kind() == reflect.Struct {
			variableList = append(variableList, exportVar(name, f.Interface())...)
			continue
		}

		var str string
		switch v := f.Interface().(type) {
		case string:
			str = v
		case int, int32, int64:
			str = fmt.Sprintf("%d", v)
		case float32, float64:
			str = fmt.Sprintf("%f", v)
		}
		variableList = append(variableList, name+"="+str)
	}
	return variableList
}

// ExecScript executes a specific script, with all information in the struct passed as 'i' exported
// as environment variables
func (j *Job) ExecScript(cfg *Config) error {
	duration := cfg.Jobs.MaxExecutionTime
	timeoutCtx, _ := context.WithTimeout(j.ctx, duration)

	var err error
	cmd := exec.CommandContext(timeoutCtx, j.Script)
	cmd.Env = exportVar("", j.Event)

	outputFileName := filepath.Join(j.Folder, "logs")
	outputFile, err := os.Create(outputFileName)
	if err != nil {
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	cleanup := func(remove bool) {
		outputFile.Close()
		if remove {
			os.Remove(outputFileName)
		}
	}

	if err := cmd.Start(); err != nil {
		cleanup(true)
		return err
	}

	j.SetStatus(StatusExecuting)

	wg := sync.WaitGroup{}
	wg.Add(2)

	scan := func(input io.Reader, f *os.File, prefix string) {
		lines := bufio.NewScanner(input)

		for lines.Scan() {
			if prefix != "" {
				f.WriteString(prefix)
			}
			f.Write(lines.Bytes())
			f.WriteString("\n")
		}
		wg.Done()
	}

	// We're interleaving stdout and stderr in the same file,
	// but we want to be able to highlight stderr differently, so we add a prefix
	// to all lines coming from stderr
	go scan(stderr, outputFile, "[[stderr]]")
	go scan(stdout, outputFile, "")

	defer cleanup(false)

	// According to the docs, we should not call cmd.Wait until
	// we've finished reading from stderr/stdout
	wg.Wait()

	err = cmd.Wait()
	if err != nil {
		select {
		case <-j.ctx.Done():
			return errExecCancelled
		case <-timeoutCtx.Done():
			return errExecTimedOut
		default:
		}
		return err
	}

	return nil
}
