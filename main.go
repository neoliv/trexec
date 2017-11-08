package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"
)

var myUsage = func() {
	c := path.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "Usage for %s:\n", c)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
Display statistics about exec() system calls.
Note that you need to have root privileges.
You can ask for an updated summary by sending SIGUSR1 to the process or let it do a periodic output with the -i flag.

eg: %s -i 30s -o /tmp/%s.out
  This will store a summary every 30s in the %s.out file.
  Every time you send a SIGUSR1 to the process (eg: pkill -10 %s), you will also get a fresh summary.

Notes about the displayed informations:

The header should be self explanatory.

The histogramm helps understand the processes execution time distribution. Every time a process dies its (wall clock) execution time is accounted in a power of 10 ns scale.

The first list displays statistics on a per command basis. The most frequently exec()ed commands or the longest (wall clock) commands.
eg: awk: 53.15%% (60641) 298.16e/s 6.65313107s (15.01%)
  Meaning that awk is the most often exec()ed command (53%%) on the server.
  It has been started 60641 times during this %s session.
  It is (on average) exec()ed 298 times per second.
  It's total wall clock execution time is 6.6s for 15% of the execution time of all processes that were execed/exited during this session.
  Note that the times used are exit-exec timesand thus are not always relevant to the real CPU load of a process. (eg: a sleep command would account for a big chunck of execution time without using CPU time.)


The second list displays statistics for a command and all its subprocesses. Eg: the commands that are the source of the biggest number of exec() syscalls. (ie: them and all their descendants.)
This should help to find the script of hell that is forking 300 awk per second.
eg: hellscript.sh: 68.29%% (259650) 395.42e/s 6.86371307s (15.23%)
  This line means that hellscript commands (and all descendants) are exec()ing 68%% of all the processes on the server (259650 in this %s session).
  The process tree rooted at hellscript (note that there may be more than one hellscript) is calling exec() at an average rate of 395/s.
  The sum of all percentages will not be 100%% because we count every exec() event once per parent of the process (all its ancestors).
  The execution time can also indicate source of CPU load, 15% of the wall clock time is attributable to hellscript and its descendants. Note that this is not real CPU execution time but wall clock time (eg: a sleep 10s will add 10s to this metric)
Note that to clarify this list we ignode some obvious processes statistics (init, systemd, ...)
 
You can sort commands by number of exec() calls or wall clock execution time (using the -s option).

This script is optimized to track all the exec()/exit() system calls on the server (using a Netlink socket from the kernel). But if the server is heavily loaded or if some proceesses are very short lived, then we may be too late to get the data from /proc/[pid]/. In this case the command is reported as (vanished).
Note that the CPU load is not proportional to the number of forked processes. But if a script is forking a lot of commands it may create a significant system load that is quite hard to track (sampling tools like top are not helping).
Only exec() events are handled, so some pathological load profiles with a lot of fork() without the usual exec() are hard to track with this tool. The header reports the number of forks without exec to help identify these rare cases. 
This (go) code should be very light (typical: <1%% CPU and <10M RSS), you can use it in production environments with no noticeable impact on performances.

If you need more help feel free to contact Olivier Arsac trexec@arsac.org.
`, c, c, c, c, c, c)
}

var sortKey string
var outfn string
var out *os.File
var interval time.Duration
var top int
var raw, clear bool

// Ccheck e, if not nil print to stderr and exit.
func check(e error) {
	if e != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", e)
		os.Exit(1)
	}
}

func parseOpts() {
	flag.Usage = myUsage
	flag.StringVar(&outfn, "o", "", "output file (default is stdout).")
	flag.StringVar(&sortKey, "s", "count", "sort criteria (count or time, default is count).")
	flag.DurationVar(&interval, "i", 0, "interval between automatic stats output (eg: 30s, 10m, 2h).")
	flag.BoolVar(&raw, "r", false, "output stats in a raw format easier to parse unsing scripts).")
	flag.BoolVar(&clear, "c", false, "clear counters every time we display stats.")
	flag.IntVar(&top, "t", 10, "number of lines in the top sections.")
	flag.Parse()
	switch sortKey {
	case "count":
		sortCriteria = scCount
	case "time":
		sortCriteria = scTime
	default:
		check(fmt.Errorf("Unknown sort criteria '%s'. Use -s 'count' or 'time'.", sortKey))
	}
	if outfn != "" {
		var err error
		out, err = os.Create(outfn)
		check(err)
	} else {
		out = os.Stderr
	}
}

// Handle signals (output stats).
func trap() {
	c := make(chan os.Signal, 1)
	//signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGTERM, os.Interrupt)
	for s := range c {
		stats()
		switch s {
		case syscall.SIGTERM, os.Interrupt:
			fmt.Fprintf(out, "Received %s Signal. Exiting.\n", s)
			os.Exit(0)
		case syscall.SIGUSR2:
			clearCounters()
		}

	}
}

// Output stats periodicaly.
func tick(i time.Duration) {
	ticker := time.NewTicker(i)
	for _ = range ticker.C {
		stats()
		if clear {
			clearCounters()
		}
	}
}

// Clean process info map periodicaly.
func tickCPIs(i time.Duration) {
	ticker := time.NewTicker(i)
	for _ = range ticker.C {
		cleanProcInfos()
	}
}

func main() {
	parseOpts()
	// Trap sigusr to display stats
	go trap()
	if interval != 0 {
		go tick(interval)
	}
	go tickCPIs(5 * 60 * time.Second) // clean process infos map every 5min
	getProcEvents()
}
