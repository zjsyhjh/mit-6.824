package paxos

//
// Paxos library, to be included in an application.
// Multiple applications will run, each including
// a Paxos peer.
//
// Manages a sequence of agreed-on values.
// The set of peers is fixed.
// Copes with network failures (partition, msg loss, &c).
// Does not store anything persistently, so cannot handle crash+restart.
//
// The application interface:
//
// px = paxos.Make(peers []string, me string)
// px.Start(seq int, v interface{}) -- start agreement on new instance
// px.Status(seq int) (Fate, v interface{}) -- get info about an instance
// px.Done(seq int) -- ok to forget all instances <= seq
// px.Max() int -- highest instance seq known, or -1
// px.Min() int -- instances before this seq have been forgotten
//

import "net"
import "net/rpc"
import "log"

import "os"
import "syscall"
import "sync"
import "sync/atomic"
import "fmt"
import (
	"math/rand"
	"strconv"
	"time"
	"math"
)


// px.Status() return values, indicating
// whether an agreement has been decided,
// or Paxos has not yet reached agreement,
// or it was agreed but forgotten (i.e. < Min()).
type Fate int

const (
	Decided   Fate = iota + 1
	Pending        // not yet decided.
	Forgotten      // decided but forgotten.
	Empty
)

const (
	InitialValue = -1
)

const (
	RejectSignal = -1
	PromisedSignal = 1
)

type Paxos struct {
	mu         sync.Mutex
	l          net.Listener
	dead       int32 // for testing
	unreliable int32 // for testing
	rpcCount   int32 // for testing
	peers      []string
	me         int // index into peers[]


	// Your data here.
	majoritySize int
	instances map[int]*InstanceState
	doneSeqs []int
	minSeq int
	maxSeq int
}


//add new type
type InstanceState struct {
	promised_n string
	accepted_n string
	accepted_v interface{}
	status Fate
}

//prepare status
type PrepareArgs struct {
	Pid int
	ProposalNum string
}

type PrepareReply struct {
	Promised int
	Accepted_N string
	Accepted_V interface{}
}

//accept status
type AcceptArgs struct {
	Pid int
	ProposalNum string
	Value interface{}
}

type AcceptReply struct {
	Accepted bool
}

//decide status
type DecidedArgs struct {
	Pid int
	ProposalNum string
	Value interface{}
}

type DecidedReply struct {
	Done int
}



//
// call() sends an RPC to the rpcname handler on server srv
// with arguments args, waits for the reply, and leaves the
// reply in reply. the reply argument should be a pointer
// to a reply structure.
//
// the return value is true if the server responded, and false
// if call() was not able to contact the server. in particular,
// the replys contents are only valid if call() returned true.
//
// you should assume that call() will time out and return an
// error after a while if it does not get a reply from the server.
//
// please use call() to send all RPCs, in client.go and server.go.
// please do not change this function.
//
func call(srv string, name string, args interface{}, reply interface{}) bool {
	c, err := rpc.Dial("unix", srv)
	if err != nil {
		err1 := err.(*net.OpError)
		if err1.Err != syscall.ENOENT && err1.Err != syscall.ECONNREFUSED {
			//fmt.Printf("paxos Dial() failed: %v\n", err1)
		}
		return false
	}
	defer c.Close()

	err = c.Call(name, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}


//add new function
func (px *Paxos) proposalIncreasingNum() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.Itoa(px.me)
}

func (px *Paxos) createMajorityPeers() []string {
	servers := make([]string, 0, px.majoritySize)
	mark := make(map[int]bool)
	count := 0
	n := len(px.peers)
	for count < px.majoritySize {
		index := rand.Intn(n)
		if _, exist := mark[index]; exist {
			continue;
		}
		mark[index] = true
		servers = append(servers, px.peers[index])
		count++
	}
	return servers
}


func (px *Paxos) sendPrepare(pid int, v interface{}, servers []string, proposalNum string) (interface{}, bool){
	args := &PrepareArgs{pid, proposalNum}
	var reply PrepareReply
	count := 0
	maxSeq, maxV := "", v
	for index, server := range servers {
		ret := false
		if index == px.me {
			err := px.Prepare(args, &reply)
			if err == nil {
				ret = true
			}
		} else {
			ret = call(server, "Paxos.Prepare", args, &reply)
		}
		if ret && reply.Promised == PromisedSignal {
			if reply.Accepted_N > maxSeq {
				maxSeq = reply.Accepted_N
				maxV = reply.Accepted_V
			}
			count++
		}
	}
	return maxV, count == px.majoritySize
}


func (px *Paxos) Prepare(args *PrepareArgs, reply *PrepareReply) error {
	px.mu.Lock()
	defer px.mu.Unlock()

	reply.Promised = RejectSignal
	promised := false
	if _, exist := px.instances[args.Pid]; exist {
		if args.ProposalNum > px.instances[args.Pid].promised_n {
			promised = true
		}
	} else {
		promised = true
		px.instances[args.Pid] = &InstanceState{promised_n:"",accepted_n:"", accepted_v:nil, status:0}
	}

	if promised {
		px.instances[args.Pid].promised_n = args.ProposalNum
		reply.Accepted_N = px.instances[args.Pid].accepted_n
		reply.Accepted_V = px.instances[args.Pid].accepted_v
		reply.Promised = PromisedSignal
	}
	return nil
}

func (px *Paxos) sendAccept(pid int, v interface{}, servers []string, proposalNum string) bool {
	args := &AcceptArgs{pid, proposalNum, v}
	var reply AcceptReply
	count := 0
	for index, server := range servers {
		ret := false
		if index == px.me {
			err := px.Accept(args, &reply)
			if err == nil {
				ret = true
			}
		} else {
			ret = call(server, "Paxos.Accept", args, &reply)
		}
		if ret && reply.Accepted {
			count++
		}
	}
	return count == px.majoritySize
}

func (px *Paxos) Accept(args *AcceptArgs, reply *AcceptReply) error {
	px.mu.Lock()
	defer px.mu.Unlock()
	if _, exist := px.instances[args.Pid]; !exist {
		px.instances[args.Pid] = &InstanceState{promised_n:"", accepted_n:"", accepted_v:nil, status:0}
	}
	if args.ProposalNum >= px.instances[args.Pid].promised_n {
		px.instances[args.Pid].promised_n = args.ProposalNum
		px.instances[args.Pid].accepted_n = args.ProposalNum
		px.instances[args.Pid].accepted_v = args.Value
		px.instances[args.Pid].status = Pending
		reply.Accepted = true
	} else {
		reply.Accepted = false
	}
	return nil
}

func (px *Paxos) sendDecided(pid int, v interface{}, proposalNum string) {
	args := &DecidedArgs{pid, proposalNum, v}
	var reply DecidedReply
	dones := make([]int, len(px.peers))
	minDone := math.MaxInt32
	ok := false
	for ok == false {
		ok = true
		for index, server := range px.peers {
			ret := false
			if index == px.me {
				err := px.Decided(args, &reply)
				if err == nil {
					ret = true
				}
			} else {
				ret = call(server, "Paxos.Decided", args, &reply)
			}
			if ret == false {
				ok = false
			} else {
				if reply.Done < minDone {
					minDone = reply.Done
				}
				dones[index] = reply.Done
			}
		}
		if ok == false {
			time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
		}
	}

	if minDone != InitialValue {
		px.mu.Lock()
		px.doneSeqs = dones
		for index, _ := range px.instances {
			if index <= minDone {
				delete(px.instances, index)
			}
		}
		px.minSeq = minDone + 1
		px.mu.Unlock()
	}
}



func (px *Paxos) Decided(args *DecidedArgs, reply *DecidedReply) error {
	px.mu.Lock()
	defer px.mu.Unlock()
	if _, exist := px.instances[args.Pid]; exist {
		px.instances[args.Pid].accepted_n = args.ProposalNum
		px.instances[args.Pid].accepted_v = args.Value
		px.instances[args.Pid].status = Decided
	} else {
		px.instances[args.Pid] = &InstanceState{promised_n:"", accepted_n:args.ProposalNum, accepted_v:args.Value, status:Decided}
	}

	if args.Pid > px.maxSeq {
		px.maxSeq = args.Pid
	}
	reply.Done = px.doneSeqs[px.me]
	return nil
}


//
// the application wants paxos to start agreement on
// instance seq, with proposed value v.
// Start() returns right away; the application will
// call Status() to find out if/when agreement
// is reached.
//
func (px *Paxos) Start(seq int, v interface{}) {
	// Your code here.
	go func() {
		if seq < px.minSeq {
			return
		}
		for {
			proposalNum := px.proposalIncreasingNum()
			servers := px.createMajorityPeers()
			maxV, ok := px.sendPrepare(seq, v, servers, proposalNum)
			if ok {
				ok = px.sendAccept(seq, maxV, servers, proposalNum)
				if ok {
					px.sendDecided(seq, maxV, proposalNum)
					break
				} else {
					time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
				}
			} else {
				time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
			}
		}
	}()
}

//
// the application on this machine is done with
// all instances <= seq.
//
// see the comments for Min() for more explanation.
//
func (px *Paxos) Done(seq int) {
	// Your code here.
	px.mu.Lock()
	defer px.mu.Unlock()
	px.doneSeqs[px.me] = seq
}

//
// the application wants to know the
// highest instance sequence known to
// this peer.
//
func (px *Paxos) Max() int {
	// Your code here.
	px.mu.Lock()
	defer px.mu.Unlock()
	return px.maxSeq
}

//
// Min() should return one more than the minimum among z_i,
// where z_i is the highest number ever passed
// to Done() on peer i. A peers z_i is -1 if it has
// never called Done().
//
// Paxos is required to have forgotten all information
// about any instances it knows that are < Min().
// The point is to free up memory in long-running
// Paxos-based servers.
//
// Paxos peers need to exchange their highest Done()
// arguments in order to implement Min(). These
// exchanges can be piggybacked on ordinary Paxos
// agreement protocol messages, so it is OK if one
// peers Min does not reflect another Peers Done()
// until after the next instance is agreed to.
//
// The fact that Min() is defined as a minimum over
// *all* Paxos peers means that Min() cannot increase until
// all peers have been heard from. So if a peer is dead
// or unreachable, other peers Min()s will not increase
// even if all reachable peers call Done. The reason for
// this is that when the unreachable peer comes back to
// life, it will need to catch up on instances that it
// missed -- the other peers therefor cannot forget these
// instances.
//
func (px *Paxos) Min() int {
	// You code here.
	px.mu.Lock()
	defer px.mu.Unlock()

	minSeq := math.MaxInt32
	for _, seq := range px.doneSeqs {
		if seq < minSeq {
			minSeq = seq
		}
	}

	if minSeq >= px.minSeq {
		for seq, _ := range px.instances {
			if seq <= minSeq {
				delete(px.instances, seq)
			}
		}
		px.minSeq = minSeq + 1
	}
	return px.minSeq
}

//
// the application wants to know whether this
// peer thinks an instance has been decided,
// and if so what the agreed value is. Status()
// should just inspect the local peer state;
// it should not contact other Paxos peers.
//
func (px *Paxos) Status(seq int) (Fate, interface{}) {
	// Your code here.
	minSeq := px.Min()
	if seq < minSeq {
		return Forgotten, nil
	}
	px.mu.Lock()
	defer px.mu.Unlock()
	if instance, ok := px.instances[seq]; ok {
		return instance.status, instance.accepted_v
	} else {
		return Empty, nil
	}
}



//
// tell the peer to shut itself down.
// for testing.
// please do not change these two functions.
//
func (px *Paxos) Kill() {
	atomic.StoreInt32(&px.dead, 1)
	if px.l != nil {
		px.l.Close()
	}
}

//
// has this peer been asked to shut down?
//
func (px *Paxos) isdead() bool {
	return atomic.LoadInt32(&px.dead) != 0
}

// please do not change these two functions.
func (px *Paxos) setunreliable(what bool) {
	if what {
		atomic.StoreInt32(&px.unreliable, 1)
	} else {
		atomic.StoreInt32(&px.unreliable, 0)
	}
}

func (px *Paxos) isunreliable() bool {
	return atomic.LoadInt32(&px.unreliable) != 0
}

//
// the application wants to create a paxos peer.
// the ports of all the paxos peers (including this one)
// are in peers[]. this servers port is peers[me].
//
func Make(peers []string, me int, rpcs *rpc.Server) *Paxos {
	px := &Paxos{}
	px.peers = peers
	px.me = me

	// Your initialization code here.
	px.majoritySize = len(px.peers) / 2 + 1
	px.minSeq = 0
	px.maxSeq = InitialValue
	px.doneSeqs = make([]int, len(px.peers))

	for i := 0; i < len(px.peers); i++ {
		px.doneSeqs[i] = InitialValue
	}

	px.instances = make(map[int]*InstanceState)

	//Initialization done

	if rpcs != nil {
		// caller will create socket &c
		rpcs.Register(px)
	} else {
		rpcs = rpc.NewServer()
		rpcs.Register(px)

		// prepare to receive connections from clients.
		// change "unix" to "tcp" to use over a network.
		os.Remove(peers[me]) // only needed for "unix"
		l, e := net.Listen("unix", peers[me])
		if e != nil {
			log.Fatal("listen error: ", e)
		}
		px.l = l

		// please do not change any of the following code,
		// or do anything to subvert it.

		// create a thread to accept RPC connections
		go func() {
			for px.isdead() == false {
				conn, err := px.l.Accept()
				if err == nil && px.isdead() == false {
					if px.isunreliable() && (rand.Int63()%1000) < 100 {
						// discard the request.
						conn.Close()
					} else if px.isunreliable() && (rand.Int63()%1000) < 200 {
						// process the request but force discard of reply.
						c1 := conn.(*net.UnixConn)
						f, _ := c1.File()
						err := syscall.Shutdown(int(f.Fd()), syscall.SHUT_WR)
						if err != nil {
							fmt.Printf("shutdown: %v\n", err)
						}
						atomic.AddInt32(&px.rpcCount, 1)
						go rpcs.ServeConn(conn)
					} else {
						atomic.AddInt32(&px.rpcCount, 1)
						go rpcs.ServeConn(conn)
					}
				} else if err == nil {
					conn.Close()
				}
				if err != nil && px.isdead() == false {
					fmt.Printf("Paxos(%v) accept: %v\n", me, err.Error())
				}
			}
		}()
	}


	return px
}
