#include <sys/socket.h>
#include <linux/netlink.h>
#include <linux/connector.h>
#include <linux/cn_proc.h>
#include <signal.h>
#include <errno.h>
#include <stdbool.h>
#include <unistd.h>
#include <string.h>
#include <stdlib.h>
#include <stdio.h>

/* Go handlers for process events. */
extern void goProcEventFork(int, int, unsigned long);
extern void goProcEventExec(int, unsigned long,unsigned long,unsigned long);
extern void goProcEventExit(int, unsigned long);

// TODO direct access to these variables from Go? (had duplicate declaration error durint my tests)
static unsigned long nbforkev=0; // count fork events
static unsigned long nbexitev=0; // count exit events

static int nl_connect()
{
  int rc;
  int nl_sock;
  struct sockaddr_nl sa_nl;

  nl_sock = socket(PF_NETLINK, SOCK_DGRAM, NETLINK_CONNECTOR);
  if (nl_sock == -1) {
    perror("netlink socket");
    return -1;
  }

  sa_nl.nl_family = AF_NETLINK;
  sa_nl.nl_groups = CN_IDX_PROC;
  sa_nl.nl_pid = getpid();

  rc = bind(nl_sock, (struct sockaddr *)&sa_nl, sizeof(sa_nl));
  if (rc == -1) {
    perror("netlink bind");
    close(nl_sock);
    return -1;
  }

  return nl_sock;
}


static int set_proc_ev_listen(int nl_sock, bool enable)
{
  int rc;
  struct __attribute__ ((aligned(NLMSG_ALIGNTO))) {
    struct nlmsghdr nl_hdr;
    struct __attribute__ ((__packed__)) {
      struct cn_msg cn_msg;
      enum proc_cn_mcast_op cn_mcast;
    };
  } nlcn_msg;

  memset(&nlcn_msg, 0, sizeof(nlcn_msg));
  nlcn_msg.nl_hdr.nlmsg_len = sizeof(nlcn_msg);
  nlcn_msg.nl_hdr.nlmsg_pid = getpid();
  nlcn_msg.nl_hdr.nlmsg_type = NLMSG_DONE;

  nlcn_msg.cn_msg.id.idx = CN_IDX_PROC;
  nlcn_msg.cn_msg.id.val = CN_VAL_PROC;
  nlcn_msg.cn_msg.len = sizeof(enum proc_cn_mcast_op);

  nlcn_msg.cn_mcast = enable ? PROC_CN_MCAST_LISTEN : PROC_CN_MCAST_IGNORE;

  rc = send(nl_sock, &nlcn_msg, sizeof(nlcn_msg), 0);
  if (rc == -1) {
    perror("netlink send");
    return -1;
  }

  return 0;
}


static int handle_proc_ev(int nl_sock)
{
  int rc;
  struct __attribute__ ((aligned(NLMSG_ALIGNTO))) {
    struct nlmsghdr nl_hdr;
    struct __attribute__ ((__packed__)) {
      struct cn_msg cn_msg;
      struct proc_event proc_ev;
    };
  } nlcn_msg;

  while (1) {
    rc = recv(nl_sock, &nlcn_msg, sizeof(nlcn_msg), 0);
    if (rc == 0) {
      return 0;
    } else if (rc == -1) {
      if (errno == EINTR) {
		continue;
	  }
      else if (errno == ENOBUFS) {
		// TODO: set a flag to inform go that we missed one or more events due to buffer overflow. Need a full reinit of the processes state.
		continue;
      }
	  perror("netlink recv");
      return -1;
    }
	unsigned long ts = nlcn_msg.proc_ev.timestamp_ns;
    switch (nlcn_msg.proc_ev.what) {
    // The forks are not the relevant event so we only count them.
    case PROC_EVENT_FORK:
	  nbforkev++;
	  //goProcEventFork(nlcn_msg.proc_ev.event_data.fork.parent_pid,nlcn_msg.proc_ev.event_data.fork.child_pid, ts);
      /*printf("fork: parent tid=%d pid=%d -> child tid=%d pid=%d\n",
	     nlcn_msg.proc_ev.event_data.fork.parent_pid,
	     nlcn_msg.proc_ev.event_data.fork.parent_tgid,
	     nlcn_msg.proc_ev.event_data.fork.child_pid,
	     nlcn_msg.proc_ev.event_data.fork.child_tgid);
      */
      break;
    case PROC_EVENT_EXEC:
      goProcEventExec(nlcn_msg.proc_ev.event_data.exec.process_pid, ts, nbforkev, nbexitev);
      /*printf("exec: tid=%d pid=%d\n",
	     nlcn_msg.proc_ev.event_data.exec.process_pid,
	     nlcn_msg.proc_ev.event_data.exec.process_tgid);
      */
      break;
    case PROC_EVENT_EXIT:
      nbexitev++;
	  goProcEventExit(nlcn_msg.proc_ev.event_data.exit.process_pid, ts);
      /*printf("exit: tid=%d pid=%d exit_code=%d\n",
	     nlcn_msg.proc_ev.event_data.exit.process_pid,
	     nlcn_msg.proc_ev.event_data.exit.process_tgid,
	     nlcn_msg.proc_ev.event_data.exit.exit_code);
      */
      break;
      /*default:
      printf("unhandled proc event\n");
      break;
      */
    }
  }

  return 0;
}


static char* errMsg()
{
	return strerror(errno);
}

static int getProcEvents(){
  int nl_sock;
  int rc;

  nl_sock = nl_connect();
  if (nl_sock == -1)
    return -1;

  rc = set_proc_ev_listen(nl_sock, true);
  if (rc == -1) {
    close(nl_sock);
    return -1;
  }

  rc = handle_proc_ev(nl_sock);
  if (rc == -1) {
    close(nl_sock);
    return -1;
  }

  set_proc_ev_listen(nl_sock, false);
  close(nl_sock);
}
