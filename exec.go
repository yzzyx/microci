package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
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
		fmt.Println(name, str)
		variableList = append(variableList, name+"="+str)
	}
	return variableList
}

type Output struct {
	OutputFile string
	Error      error
	Finished   bool
}

// ExecScript executes a specific script, with all information in the struct passed as 'i' exported
// as environment variables
func ExecScript(ctx context.Context, script string, i interface{}) (*Output, error) {
	cmd := exec.CommandContext(ctx, script)
	cmd.Env = exportVar("", i)

	// FIXME - write to correct location
	outputFile, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	out := Output{
		OutputFile: outputFile.Name(),
	}

	cleanup := func(remove bool) {
		outputFile.Close()
		if remove {
			os.Remove(out.OutputFile)
		}
	}

	if err := cmd.Start(); err != nil {
		cleanup(true)
		return nil, err
	}

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

	go func() {
		defer cleanup(false)

		// According to the docs, we should not call cmd.Wait until
		// we've finished reading from stderr/stdout
		wg.Wait()

		out.Error = cmd.Wait()
		out.Finished = true
	}()

	return &out, nil
}
