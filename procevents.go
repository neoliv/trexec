package main

/*
#include "procevents.c"
*/
import "C"

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const (
	scCount = iota
	scTime  = iota
)

var scStrings = [2]string{}

var sortCriteria = scCount
var vanishedCount uint64 // number of failed read in /proc/#/stat == vanished proces count.
var removedCount uint64  // how many removed processes.

// This process start time.
var start time.Time
var ehist = [32]uint64{} // execution time histogram

// number of different command (name).
var nbforkev uint64 // count fork events.
var nbExecEv uint64 // count exec events.
var nbExitEv uint64 // count exit events.

type cmdInfo struct {
	cmd   string // command
	subec uint64 // count how many sub processes this command has owned (all descendents)
	subet uint64 // cummulative execution time in sub processes.
	spid  int    // pid that triggered the last tree climb.
	ec    uint64 // number of times this command has been exec'ed().
	et    uint64 // exec time in all instances of this command.
	tsub  uint64 // cimmulative time in all sub processes of this command.
}

type procInfo struct {
	pid  int       // this process PID
	ppid int       // parent PID
	ppi  *procInfo // Parent process info.
	ci   *cmdInfo  // Info about all processes sharing this command.
	st   uint64    // start time.
}

var mutInfos = sync.Mutex{} // protect the *info maps

// For every command stores its ifnormations.
var cmdInfos = map[string](*cmdInfo){}

// For every PID stores its informations.
var procInfos = map[int](*procInfo){}

func init() {
	start = time.Now()
	scStrings[scCount] = "number of exec"
	scStrings[scTime] = "execution time"
}

// Reset all counters. (like a fresh start)
func clearCounters() {
	procInfos = map[int](*procInfo){}
	cmdInfos = map[string](*cmdInfo){}
	ehist = [32]uint64{} // execution time histogram
	nbforkev = 0
	nbExecEv = 0
	nbExitEv = 0
	start = time.Now()
}

// Display the per process exec stats.
func statsExec(dts float64) {
	printSep(out, " top %d commands sorted by %s ", top, scStrings[sortCriteria])
	n := map[uint64][](*cmdInfo){}
	var a UInt64Slice
	mutInfos.Lock()
	for _, ci := range cmdInfos {
		var ui uint64
		switch sortCriteria {
		case scCount:
			ui = ci.ec
		case scTime:
			ui = ci.et
		}
		if ui != 0 {
			n[ui] = append(n[ui], ci)
		}
	}
	mutInfos.Unlock()
	for k := range n {
		a = append(a, k)
	}
	sort.Sort(sort.Reverse(a))
	var sec, set uint64
	var i int
	for _, k := range a {
		for _, ci := range n[k] {
			sec = sec + ci.ec
			set = set + ci.et
		}
	}
	for _, k := range a {
		for _, ci := range n[k] {
			cmd := ci.cmd
			if cmd == "" {
				cmd = "(vanished)"
			}
			ec := ci.ec
			ecpc := (float32(ec*100) / float32(sec))
			eps := (float64(ec) / dts)
			et := ci.et
			if et != 0 {
				etpc := (float32(et*100) / float32(set))
				var det = time.Duration(et)
				if raw {
					fmt.Fprintf(out, "pp:%s:%.2f:%d:%.2f:%s:%.2f\n", cmd, ecpc, ec, eps, det.String(), etpc)
				} else {
					fmt.Fprintf(out, "%s: %.2f%% (%d) %.2fe/s %s (%.2f%%)\n", cmd, ecpc, ec, eps, det.String(), etpc)
				}
			} else {
				if raw {
					fmt.Fprintf(out, "pp:%s:%.2f:%d:%.2f::\n", cmd, ecpc, ec, eps)
				} else {
					fmt.Fprintf(out, "%s: %.2f%% (%d) %.2fe/s\n", cmd, ecpc, ec, eps)
				}
			}
			i++
			if i > top {
				return
			}
		}
	}
}

// Display the sub process stats
func statsSub(dts float64) {
	printSep(out, " top %d commands sorted by sum of subprocesses %s ", top, scStrings[sortCriteria])
	n := map[uint64][](*cmdInfo){}
	var a UInt64Slice
	mutInfos.Lock()
	for _, ci := range cmdInfos {
		if ci.subec == 0 || ci.cmd == "" || ci.cmd == "init" || ci.cmd == "systemd" {
			// No sub processes or we know that every process is sub of init, no need to mess stats with this one.
			continue
		}
		var ui uint64
		switch sortCriteria {
		case scCount:
			ui = ci.subec
		case scTime:
			ui = ci.subet
		}
		if ui != 0 {
			n[ui] = append(n[ui], ci)
		}
	}
	mutInfos.Unlock()
	for k := range n {
		a = append(a, k)
	}
	sort.Sort(sort.Reverse(a))
	var i int
	for _, k := range a {
		for _, ci := range n[k] {
			cmd := ci.cmd
			if cmd == "" {
				cmd = "(vanished)"
			}
			if raw {
				fmt.Fprintf(out, "cp:%s:%.2f%:%d:%.2f\n", cmd, (float32(ci.subec*100) / float32(nbExecEv)), ci.subec, (float64(ci.subec) / dts))
			} else {
				fmt.Fprintf(out, "%s: %.2f%% (%d) %.2fe/s\n", cmd, (float32(ci.subec*100) / float32(nbExecEv)), ci.subec, (float64(ci.subec) / dts))
			}
			i++
			if i > top {
				return
			}
		}
	}
}

// Display the histogram for command execution time.
func statsEHist(dts float64) {
	var firsti, lasti int
	var s uint64 // sum of all values in the histogram.
	firsti = -1
	for l := 0; l < len(ehist); l++ {
		if ehist[l] != 0 {
			lasti = l
			s += ehist[l]
			if firsti < 0 {
				firsti = l // index of the first non 0 sample
			}
		}
	}
	if firsti < 0 {
		// nothing in the histogram, skip its display.
		return
	}
	printSep(out, " command execution time histogram (%d executed commands) ", nbExitEv)
	fmt.Fprintf(out, "|")
	p := 1
	for l := 0; l <= lasti; l++ {
		p *= 10
		if l >= firsti {
			fmt.Fprintf(out, " <%5s |", time.Duration(p).String())
		}
	}
	fmt.Fprintf(out, "\n|")
	for l := firsti; l <= lasti; l++ {
		if ehist[l] != 0 {
			p5 := math.Ceil(float64(10000*ehist[l]) / float64(s))
			pc := p5 / 100
			pcs := strconv.FormatFloat(pc, 'f', -1, 64)
			//pcs := fmt.Sprintf("%4f", pc)
			fmt.Fprintf(out, "%6s%% |", pcs)
		} else {
			fmt.Fprintf(out, "        |")

		}
	}
	fmt.Fprintf(out, "\n")
}

// Display a summary of gathered statitistics about evec() events.
func stats() {
	dt := time.Since(start)
	dts := dt.Seconds()
	getTermDimensions() // Update the term width every display.
	printSep(out, "")
	hn, _ := os.Hostname()
	fmt.Fprintf(out, "hostname:           %s\n", hn)
	fmt.Fprintf(out, "date:               %s\n", time.Now())
	fmt.Fprintf(out, "time since start:   %s\n", time.Duration.String(dt))
	fmt.Fprintf(out, "total exec calls:   %d (%.2fe/s)\n", nbExecEv, float32(nbExecEv)/float32(dts))
	fmt.Fprintf(out, "forks w/o exec:     %d (%.2ff/s)\n", nbforkev-nbExecEv, float32(nbforkev-nbExecEv)/float32(dts))
	fmt.Fprintf(out, "number of comamnds: %d\n", len(cmdInfos))
	fmt.Fprintf(out, "removed/vanished:   %d/%d\n", removedCount, vanishedCount)
	statsExec(dts)
	if !raw {
		statsEHist(dts)
	}
	statsSub(dts)
	printSep(out, "")
}

// Extract the command (and ppid) from /proc/[pid]/stat
func getProcessStat(pid int) (string, int) {
	fn := fmt.Sprintf("/proc/%d/stat", pid)
	s, err := fastRead(fn)
	sl := len(s)
	if err != nil || sl == 0 {
		vanishedCount++
		return "", -1
	}
	var f int // field number (0 is pid)
	var i64 int64
	var cmd string
	for i := 0; i < sl; i++ {
		//fmt.Fprintf(out,"f:%d i:%d c:%c\n", f, i, s[i])
		switch f {
		case 1: // 1 tcomm
			i++ // Skip the '('.
			cmd, i = fastParseUntil(s, i, ')')
		case 3: // 3 ppid
			i64, i = fastParseInt(s, i)
			return cmd, int(i64)
		default: // Skip this field.
			i++
			for ; i < sl; i++ {
				if s[i] == ' ' {
					break
				}
			}
		}
		// Assume one and only one ' '  between fields.
		f++
	}
	return "", -1
}

//export goProcEventFork
// This method is not called anymore. (see procevent.c for the rational)
func goProcEventFork(cppid, cpid C.int, cts C.ulong) {
	cmd, _ := getProcessStat(int(cpid))
	fmt.Fprintf(out, "Fork: pid=%d ppid=%d cmd=[%s]\n", cpid, cppid, cmd)
}

// Remove all dead processes from the global procInfos map.
// The exit event callback should handle this but in some cases we may miss events.
func cleanProcInfos() {
	mutInfos.Lock()
	for pid := range procInfos {
		process, _ := os.FindProcess(pid) // On UNIX always success.
		err := process.Signal(syscall.Signal(0))
		if err != nil {
			delete(procInfos, pid)
			removedCount++
		}
	}
	mutInfos.Unlock()
}

// Create a new PID info struct, add it to the global map.
// If need be create/add a cmdInfo correspondig to PID cmd.
// Assumes the global maps are locked.
func makeProcInfo(pid int, vanished bool) *procInfo {
	// Get infos for this unknown PID.
	cmd, ppid := getProcessStat(pid)
	if cmd == "" { // Missed the /proc/pid file, we have no pertinent data to store, skip the map entry.
		if vanished == false {
			return nil
		}
	}
	// Create the structs even if process vanished.
	var ci *cmdInfo
	var known bool
	if ci, known = cmdInfos[cmd]; known {
		ci.ec++
	} else { // new command
		ci = &cmdInfo{cmd: cmd, ec: 1}
		cmdInfos[cmd] = ci
	}
	// New global procInfos map entry.
	pi := &procInfo{pid: pid, ppid: ppid, ci: ci}
	procInfos[pid] = pi
	return pi
}

//export goProcEventExec
func goProcEventExec(cpid C.int, cts, nf, ne C.ulong) {
	pid := int(cpid)
	nbforkev = uint64(nf)
	nbExecEv++ // this event
	nbExitEv = uint64(ne)
	mutInfos.Lock()
	pi := makeProcInfo(pid, true)
	pi.st = uint64(cts) // event stamp is process start time.
	// defer Unlock() is slower than explicit call but need to be cautious with stray returns.

	// Climb process tree up to its root (init)
	// For every ancestor of pid we increment its count of subprocesses.
	spid := pid // initial PID from where we start
	for {
		if pid <= 1 {
			// We are at the process tree root (init)
			mutInfos.Unlock()
			return
		}
		// Climb one parent process up.
		var ppi *procInfo
		if pi.ppi != nil {
			ppi = pi.ppi
		} else {
			// Pointer to parent not yet ready.
			var known bool
			if ppi, known = procInfos[pi.ppid]; !known {
				ppi = makeProcInfo(pi.ppid, false)
				if ppi == nil {
					// No more info about parent process. Stop climbing.
					mutInfos.Unlock()
					return
				}
			}
			pi.ppi = ppi
		}

		ci := ppi.ci
		if ci.spid != spid {
			// This command sub processes count has not already been incremented for the current spid (original process pid in the exec() event)
			// The thing we want to avoid in the below example is incrementing twice the bash count of subprocesses during the grep exec() event.
			// grep(spid) <- bash <- find <- bash
			//
			ci.subec++
			ci.spid = spid
		}
		pi = ppi
		pid = pi.pid
	}
	mutInfos.Unlock()
}

//export goProcEventExit
func goProcEventExit(cpid C.int, cts C.ulong) {
	//fmt.Fprintf(out, "Exit: pid=%d\n", pid)
	pid := int(cpid)
	dt := uint64(cts) // death time stamp.
	mutInfos.Lock()
	if pi, known := procInfos[pid]; known {
		delete(procInfos, pid)
		ci := pi.ci
		if pi.st != 0 {
			et := dt - pi.st // death - start == execution time
			ci.et += et
			i := int(math.Log10(float64(et)))
			//fmt.Printf("%d %d (%d/%d)\n", i, et, len(ehist))
			ehist[i]++
			// Add this execution time to all parent process command infos.
			spid := pid
			for ; pi != nil; pi = pi.ppi {
				if pi.ci.spid != spid {
					pi.ci.subet += et
					pi.ci.spid = spid
				}
			}
		}
	}
	mutInfos.Unlock()
	removedCount++
}

// Get process events directly from the Linux kernel (via tne netlink. No lag, no missed events, ... Far superior to any scan based algorithm but not portable.
func getProcEvents() {
	// Set a high scheduling priority to give this process to better chances to access /proc/[pid]/stat fast enough once it gets a netlink exec() event.
	syscall.Setpriority(syscall.PRIO_PROCESS, 0, -20)

	// This C function will connect to the kernel and wait for all events.
	// Events will be handled by callbacks in go. (see goProcEvent* functions above(.
	cr := C.getProcEvents() // This call will not return unless an error occurs (loop on select)
	if cr == -1 {
		fmt.Fprintf(os.Stderr, "Unable to set the Netlink socket properly.\nRemember that you need root privileges to do that.\n")
	}
}
