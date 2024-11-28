package wait

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

func ForResult(t *testing.T, act func(), assert func() error, tries int, dur time.Duration) {
	factor := 1
	if os.Getenv("CI") != "" { // wait longer in Github runners
		factor = 5
	}
	t.Helper()
	if act != nil {
		act()
	}
	var err error
	for i := 0; i <= tries; i++ {
		dur = time.Duration(factor * int(dur))
		if err = assert(); err == nil {
			return
		}
		time.Sleep(dur)
	}
	t.Fatalf("timeout waiting for result: %v", err)
}

func ForLog(t *testing.T, buf *bytes.Buffer, assert func(string) bool, dur time.Duration) {
	t.Helper()
	ForResult(t, nil, func() error { return retrieveMsg(t, buf, assert) }, 3, dur)
}

func retrieveMsg(t *testing.T, buf *bytes.Buffer, assert func(string) bool) error {
	t.Helper()
	if _, err := buf.ReadBytes('{'); err == nil {
		if err := buf.UnreadByte(); err != nil {
			t.Fatal(err)
		}
	}
	b := bytes.NewReader(buf.Bytes())
	scanner := bufio.NewScanner(b)
	for scanner.Scan() {
		line := scanner.Bytes()
		var m struct {
			Msg string
		}
		if err := json.NewDecoder(bytes.NewReader(line)).Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode console logs: %v", err)
		}
		if assert(m.Msg) {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	return errors.New("log not found")
}

func ForLogFields(t *testing.T, buf *bytes.Buffer, assert func(map[string]any) bool, dur time.Duration) {
	t.Helper()
	ForResult(t, nil, func() error { return retrieveField(t, buf, assert) }, 3, dur)
}

func retrieveField(t *testing.T, buf *bytes.Buffer, assert func(map[string]any) bool) error {
	t.Helper()
	if _, err := buf.ReadBytes('{'); err == nil {
		if err := buf.UnreadByte(); err != nil {
			t.Fatal(err)
		}
	}
	b := bytes.NewReader(buf.Bytes())
	scanner := bufio.NewScanner(b)
	for scanner.Scan() {
		line := scanner.Bytes()
		var m map[string]interface{}
		if err := json.NewDecoder(bytes.NewReader(line)).Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode console logs: %v", err)
		}
		if assert(m) {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	return errors.New("log not found")
}

// Func will call passed function at an interval and return nil
// as soon this function returns true.
// If timeout is reached before the passed in function returns true
// an error is returned.
//
// Taken from github.com/open-policy-agent/opa/utils/wait.go
// Copyright 2020 The OPA Authors.  All rights reserved.
func Func(fun func() bool, interval, timeout time.Duration) error {
	if fun() {
		return nil
	}
	ticker := time.NewTicker(interval)
	timer := time.NewTimer(timeout)
	defer ticker.Stop()
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return fmt.Errorf("timeout")
		case <-ticker.C:
			if fun() {
				return nil
			}
		}
	}
}
