package nca

import (
	"bytes"
	"fmt"
	log "gopkg.in/inconshreveable/log15.v2"
	"os/exec"
	"syscall"
	"time"
)

// Check describes an individual nagios check.
type Check struct {
	Name    string
	Command string
	Args    []string // Command after being parsed according to shell quoting rules

	Interval int
	Retry    int
	Timeout  int
}

// checker is responsible for the scheduling and running of checks.
type checker struct {
	publish chan *checkResult // The publish channel exposed by the Runner
	done    chan struct{}     // Used for signalling goroutines that we're shutting down
	checks  map[string]Check  // The checks that need to be run
	log     log.Logger
}

// checkResult describes the result of a given check.
type checkResult struct {
	Name       string
	Output     []byte
	Returncode int
}

func (c *checker) Start() error {
	c.log = Log.New("component", "checker")
	c.log.Info("Checker starting")
	c.done = make(chan struct{})

	for _, check := range c.checks {
		go c.checkRoutine(check)
	}
	//go c.run()
	return nil
}

func (c *checker) Stop() error {
	c.log.Info("Checker stopping")
	close(c.done)
	return nil
}

// checkRoutine runs a given check at the configured interval. It is meant to
// be run in its own goroutine.
func (c *checker) checkRoutine(check Check) {
	l := c.log.New("check", check.Name)
	l.Debug("Check scheduled", "interval", check.Interval, "retry_interval", check.Retry, "command", check.Command, "timeout", check.Timeout)
	//time.Sleep(check.Retry * time.Second)
	//c.publish <- runCheck(Check{})
	delay := time.Duration(check.Retry) * time.Second
	for {
		select {
		case <-time.After(delay):
			l.Debug("Executing check")
			result := runCheck(check)
			c.publish <- result
			if result.Returncode == 0 {
				delay = time.Duration(check.Interval) * time.Second
			} else {
				delay = time.Duration(check.Retry) * time.Second
			}
			l.Debug("Check complete", "returncode", result.Returncode, "next_check_after", delay)
		case <-c.done:
			l.Debug("Cancelling further checks")
			return
		}
	}

}

// runCheck runs a given check and returns the result of its execution.
func runCheck(check Check) *checkResult {
	//dummy := &checkResult{
	//name:       check.Name,
	//output:     []byte("Fake output"),
	//returncode: 1,
	//}
	//return dummy
	checkLog := Log.New("check", check.Name)
	cmd := exec.Command(check.Args[0], check.Args[1:]...)
	result := &checkResult{Name: check.Name}
	var b bytes.Buffer
	cmd.Stdout = &b
	cmd.Stderr = &b

	if err := cmd.Start(); err != nil {
		checkLog.Error("Couldn't execute check command", "error", err)
		result.Returncode = StateCritical
		result.Output = []byte("Check execution failed: " + err.Error())
		return result
	}

	done := make(chan error)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			result.Returncode = StateOk
			result.Output = b.Bytes()
			checkLog.Info("Check executed successfully", "returncode", result.Returncode)
		} else {
			if exiterr, ok := err.(*exec.ExitError); ok {
				if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
					result.Returncode = status.ExitStatus()
					result.Output = b.Bytes()
					checkLog.Info("Check executed successfully", "returncode", result.Returncode)
				} else {
					// FIXME: When/how does this case pop up?
					result.Returncode = StateCritical
					result.Output = []byte(exiterr.Error())
					checkLog.Warn("Check command failed unexpectedly", "error", err)
				}
			} else {
				result.Returncode = StateCritical
				result.Output = []byte("Check execution failed: " + err.Error())
				checkLog.Warn("Check execution failed", "error", err)
				return result
			}
		}
	case <-time.After(time.Duration(check.Timeout) * time.Second):
		cmd.Process.Kill()
		result.Returncode = StateCritical
		result.Output = []byte(fmt.Sprintf("Process %s was killed after %d second timeout", check.Args[0], check.Timeout))
		checkLog.Warn("Check timeout exceeded")
		return result
	}
	return result
}