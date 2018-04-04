# trexec

You may want to look at topfast. trexec is a first attempt to get at short lived processes stats but I consider topfast superior.  

This tool helps when top is not able to find the origin of a CPU load on your Linux system.  
It relies on netlink interface to the kernel to track all processes (even the very short lived ones). For example if there is a script forking grep, perl, awk, expr, basename and so on the CPU load may be high but these processes will stay under the radar of usual sampling tools like top. trexec will gather data and publish a summary of the most executed commands and their origin.  
Hope this helps.  

Install:  

go get github.com/neoliv/trexec


Example:

Here is an output sample:  
```
hostname:           crawling01
date:               2017-11-09 12:02:55.702423786 +0000 UTC
time since start:   2m48.071796286s
total exec calls:   66942 (398.29e/s)
forks w/o exec:     6681 (39.76f/s)
number of comamnds: 153
----- top 10 commands sorted by number of exec ---------------------------------
tr: 42.34% (42324) 251.82e/s 31.882631999s (3.14%)
bc: 24.31% (24294) 144.55e/s
grep: 6.87% (6870) 40.88e/s 23.635982653s (2.32%)
wc: 4.84% (4840) 28.80e/s 8.95871531s (0.88%)
cut: 4.46% (4459) 26.53e/s 2.686681305s (0.26%)
hog.sh: 3.30% (3301) 19.64e/s 26.316595303s (2.59%)
sed: 1.79% (1787) 10.63e/s 11.564354125s (1.14%)
check_active_co: 1.45% (1454) 8.65e/s 6.793365866s (0.67%)
expr: 0.81% (810) 4.82e/s 700.056166ms (0.07%)
basename: 0.65% (651) 3.87e/s 2.707907174s (0.27%)
awk: 0.62% (618) 3.68e/s 2.230339281s (0.22%)
----- top 10 commands sorted by sum of subprocesses number of exec -------------
hog.sh: 88.66% (59351) 353.13e/s
monitor.sh: 77.80% (52083) 309.89e/s
start_schedule.: 77.78% (52070) 309.81e/s
ApplicationAgent: 14.47% (9687) 57.64e/s
su: 14.34% (9601) 57.12e/s
```  

With the above output you see that the hog.sh is responsible for a lot of short lived processes and that it could be a good idea to rewrite that script.

See the -h below:  
```
Usage for trexec:
  -c clear counters every time we display stats.
  -i duration
    	interval between automatic stats output (eg: 30s, 10m, 2h).
  -o string
    	output file (default is stdout).
  -r	output stats in a raw format easier to parse unsing scripts).
  -s string
    	sort criteria (count or time, default is count). (default "count")
  -t int
    	number of lines in the top sections. (default 10)

Display statistics about exec() system calls.
Note that you need to have root privileges.
You can ask for an updated summary by sending SIGUSR1 to the process or let it do a periodic output with the -i flag.

eg: trexec -i 30s -o /tmp/trexec.out
  This will store a summary every 30s in the trexec.out file.
  Every time you send a SIGUSR1 to the process (eg: pkill -10 trexec), you will also get a fresh summary.

Notes about the displayed informations:

The header should be self explanatory.

The histogramm helps understand the processes execution time distribution. Every time a process dies its (wall clock) execution time is accounted in a power of 10 ns scale.

The first list displays statistics on a per command basis. The most frequently exec()ed commands or the longest (wall clock) commands.
eg: awk: 53.15% (60641) 298.16e/s 6.65313107s (15.01%!)(string=trexec)
  Meaning that awk is the most often exec()ed command (53%) on the server.
  It has been started 60641 times during this trexec session.
  It is (on average) exec()ed 298 times per second.
  It's total wall clock execution time is 6.6s for 15%!o(MISSING)f the execution time of all processes that were execed/exited during this session.
  Note that the times used are exit-exec timesand thus are not always relevant to the real CPU load of a process. (eg: a sleep command would account for a big chunck of execution time without using CPU time.)


The second list displays statistics for a command and all its subprocesses. Eg: the commands that are the source of the biggest number of exec() syscalls. (ie: them and all their descendants.)
This should help to find the script of hell that is forking 300 awk per second.
eg: hellscript.sh: 68.29% (259650) 395.42e/s 6.86371307s (15.23%!)(MISSING)
  This line means that hellscript commands (and all descendants) are exec()ing 68% of all the processes on the server (259650 in this %!s(MISSING) session).
  The process tree rooted at hellscript (note that there may be more than one hellscript) is calling exec() at an average rate of 395/s.
  The sum of all percentages will not be 100% because we count every exec() event once per parent of the process (all its ancestors).
  The execution time can also indicate source of CPU load, 15%!o(MISSING)f the wall clock time is attributable to hellscript and its descendants. Note that this is not real CPU execution time but wall clock time (eg: a sleep 10s will add 10s to this metric)
Note that to clarify this list we ignode some obvious processes statistics (init, systemd, ...)
 
You can sort commands by number of exec() calls or wall clock execution time (using the -s option).

This script is optimized to track all the exec()/exit() system calls on the server (using a Netlink socket from the kernel). But if the server is heavily loaded or if some proceesses are very short lived, then we may be too late to get the data from /proc/[pid]/. In this case the command is reported as (vanished).
Note that the CPU load is not proportional to the number of forked processes. But if a script is forking a lot of commands it may create a significant system load that is quite hard to track (sampling tools like top are not helping).
Only exec() events are handled, so some pathological load profiles with a lot of fork() without the usual exec() are hard to track with this tool. The header reports the number of forks without exec to help identify these rare cases. 
This (go) code should be very light (typical: <1% CPU and <10M RSS), you can use it in production environments with no noticeable impact on performances.


```  

