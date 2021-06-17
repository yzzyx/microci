package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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

// ExecScript executes a specific script, with all information in the struct passed as 'i' exported
// as environment variables
func (j *Job) ExecScript() error {
	var err error
	cmd := exec.CommandContext(j.ctx, j.Script)
	cmd.Env = exportVar("", j.Event)

	cmd.Stderr, err = os.Create(filepath.Join(j.Folder, "stderr"))
	if err != nil {
		return err
	}

	cmd.Stdout, err = os.Create(filepath.Join(j.Folder, "stdout"))
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// FIXME - we should probably pipe stdout/stderr information to clients AND to files
	//wg := sync.WaitGroup{}
	//wg.Add(2)
	//stderrLines := bufio.NewScanner(stderr)
	//go func() {
	//	lineNo := 1
	//	for stderrLines.Scan() {
	//		fmt.Println("[stderr]", lineNo, stderrLines.Text())
	//		lineNo++
	//	}
	//	wg.Done()
	//}()
	//
	//stdoutLines := bufio.NewScanner(stdout)
	//go func() {
	//	lineNo := 1
	//	for stdoutLines.Scan() {
	//		fmt.Println("[stdout]", lineNo, stdoutLines.Text())
	//		lineNo++
	//	}
	//	wg.Done()
	//}()
	//
	//// According to the docs, we should not call cmd.Wait until
	//// we've finished reading from stderr/stdout
	//wg.Wait()
	if err := cmd.Wait(); err != nil {
		return err
	}

	return nil
}
