package localbinary

import (
	"bufio"
	"fmt"
	"io"
	"testing"
	"time"

	"os"

	"github.com/docker/machine/libmachine/log"
)

type FakeExecutor struct {
	stdout, stderr io.ReadCloser
	closed         bool
}

func (fe *FakeExecutor) Start() (*bufio.Scanner, *bufio.Scanner, error) {
	return bufio.NewScanner(fe.stdout), bufio.NewScanner(fe.stderr), nil
}

func (fe *FakeExecutor) Close() error {
	fe.closed = true
	return nil
}

func TestLocalBinaryPluginAddress(t *testing.T) {
	lbp := &Plugin{}
	expectedAddr := "127.0.0.1:12345"

	lbp.addrCh = make(chan string, 1)
	lbp.addrCh <- expectedAddr

	// Call the first time to read from the channel
	addr, err := lbp.Address()
	if err != nil {
		t.Fatalf("Expected no error, instead got %s", err)
	}
	if addr != expectedAddr {
		t.Fatal("Expected did not match actual address")
	}

	// Call the second time to read the "cached" address value
	addr, err = lbp.Address()
	if err != nil {
		t.Fatalf("Expected no error, instead got %s", err)
	}
	if addr != expectedAddr {
		t.Fatal("Expected did not match actual address")
	}
}

func TestLocalBinaryPluginAddressTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout test")
	}
	lbp := &Plugin{}
	lbp.addrCh = make(chan string, 1)
	go func() {
		_, err := lbp.Address()
		if err == nil {
			t.Fatalf("Expected to get a timeout error, instead got %s", err)
		}
	}()
	time.Sleep(defaultTimeout + 1)
}

func TestLocalBinaryPluginClose(t *testing.T) {
	lbp := &Plugin{}
	lbp.stopCh = make(chan bool, 1)
	go lbp.Close()
	stopped := <-lbp.stopCh
	if !stopped {
		t.Fatal("Close did not send a stop message on the proper channel")
	}
}

func TestExecServer(t *testing.T) {
	logReader, logWriter := io.Pipe()

	log.SetDebug(true)
	log.SetOutput(logWriter)

	defer func() {
		log.SetDebug(false)
		log.SetOutput(os.Stderr)
	}()

	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	fe := &FakeExecutor{
		stdout: stdoutReader,
		stderr: stderrReader,
	}

	machineName := "test"
	lbp := &Plugin{
		MachineName: machineName,
		Executor:    fe,
		addrCh:      make(chan string, 1),
		stopCh:      make(chan bool, 1),
	}

	finalErr := make(chan error)

	// Start the docker-machine-foo plugin server
	go func() {
		finalErr <- lbp.execServer()
	}()

	logScanner := bufio.NewScanner(logReader)

	// Write the ip address
	expectedAddr := "127.0.0.1:12345"
	if _, err := io.WriteString(stdoutWriter, expectedAddr+"\n"); err != nil {
		t.Fatalf("Error attempting to write plugin address: %s", err)
	}

	if addr := <-lbp.addrCh; addr != expectedAddr {
		t.Fatalf("Expected to read the expected address properly in server but did not")
	}

	// Write a log in stdout
	expectedPluginOut := "Doing some fun plugin stuff..."
	if _, err := io.WriteString(stdoutWriter, expectedPluginOut+"\n"); err != nil {
		t.Fatalf("Error attempting to write to out in plugin: %s", err)
	}

	expectedOut := fmt.Sprintf(pluginOut, machineName, expectedPluginOut)
	if logScanner.Scan(); logScanner.Text() != expectedOut {
		t.Fatalf("Output written to log was not what we expected\nexpected: %s\nactual:   %s", expectedOut, logScanner.Text())
	}

	// Write a log in stderr
	expectedPluginErr := "Uh oh, something in plugin went wrong..."
	if _, err := io.WriteString(stderrWriter, expectedPluginErr+"\n"); err != nil {
		t.Fatalf("Error attempting to write to err in plugin: %s", err)
	}

	expectedErr := fmt.Sprintf(pluginErr, machineName, expectedPluginErr)
	if logScanner.Scan(); logScanner.Text() != expectedErr {
		t.Fatalf("Error written to log was not what we expected\nexpected: %s\nactual:   %s", expectedErr, logScanner.Text())
	}

	lbp.Close()

	if err := <-finalErr; err != nil {
		t.Fatalf("Error serving: %s", err)
	}
}
