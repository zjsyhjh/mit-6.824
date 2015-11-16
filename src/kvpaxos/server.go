package kvpaxos

import "net"
import "fmt"
import "net/rpc"
import "log"
import "paxos"
import "sync"
import "sync/atomic"
import "os"
import "syscall"
import "encoding/gob"
import (
	"math/rand"
	"time"
)


const Debug = 0

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		log.Printf(format, a...)
	}
	return
}


type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	Key string
	Value string
	Type string
	Uid string
}

type KVPaxos struct {
	mu         sync.Mutex
	l          net.Listener
	me         int
	dead       int32 // for testing
	unreliable int32 // for testing
	px         *paxos.Paxos

	// Your definitions here.
	key_value map[string]string
	request map[string]int
	seq int

}


//add new function
//proposal the value, and wait until the value is decided
func (kv *KVPaxos) proposalInstance(seq int, v interface{}) interface{} {
	kv.px.Start(seq, v)
	to := time.Millisecond * 10
	for {
		status, v := kv.px.Status(seq) //return the status and accepted_v, status = (Forgotten, Pending, Decided, Empty)
		if status == paxos.Decided {
			return v
		}
		time.Sleep(to)
		if to < time.Second * 10 {
			to *= 2
		}
	}
	return ""
}

//add new function
//sync with other servers
//check status if kv.seq <= maxSeq
func (kv *KVPaxos) synchronization(maxSeq int, uid string) {
	for kv.seq <= maxSeq {
		status, v := kv.px.Status(kv.seq)
		if status == paxos.Empty {
			kv.proposalInstance(kv.seq, Op{})
			status, v = kv.px.Status(kv.seq)
		}
		to := time.Millisecond * 10
		for {
			if status == paxos.Decided {
				var op Op = v.(Op)
				if op.Type != "Get" {
					if _, exist := kv.request[op.Uid]; !exist {
						if op.Type == "Put" {
							kv.key_value[op.Key] = op.Value
						} else if op.Type == "Append" {
							kv.key_value[op.Key] += op.Value
						}
						kv.request[op.Uid] = 1
					}
				}
				break
			}
			time.Sleep(to)
			if to < time.Second * 10 {
				to *= 2
			}
			status, v = kv.px.Status(kv.seq)
		}
		kv.seq += 1
	}
	kv.px.Done(kv.seq - 1)
}


func (kv *KVPaxos) process(op Op) {
	for {
		maxSeq := kv.px.Max()
		kv.synchronization(maxSeq, op.Uid) //wait for proposal_seq <= maxSeq
		v := kv.proposalInstance(maxSeq + 1, op)
		if v == op {
			break
		}
	}
}


func (kv *KVPaxos) Get(args *GetArgs, reply *GetReply) error {
	// Your code here.

	kv.mu.Lock()
	defer kv.mu.Unlock()

	if _, exist := kv.request[args.Uid]; !exist {
		op := Op{Key:args.Key, Value:"", Type:"Get", Uid:args.Uid}
		kv.process(op)
	}

	if _, exist := kv.key_value[args.Key]; exist {
		reply.Value = kv.key_value[args.Key]
		reply.Err = OK
	} else {
		reply.Err = ErrNoKey
	}
	return nil
}

func (kv *KVPaxos) PutAppend(args *PutAppendArgs, reply *PutAppendReply) error {
	// Your code here.

	kv.mu.Lock()
	defer kv.mu.Unlock()

	if _, exist := kv.request[args.Uid]; !exist {
		op := Op{Key:args.Key, Value:args.Value, Type:args.Op, Uid:args.Uid}
		kv.process(op)
	}
	reply.Err = OK
	return nil
}

// tell the server to shut itself down.
// please do not change these two functions.
func (kv *KVPaxos) kill() {
	DPrintf("Kill(%d): die\n", kv.me)
	atomic.StoreInt32(&kv.dead, 1)
	kv.l.Close()
	kv.px.Kill()
}

// call this to find out if the server is dead.
func (kv *KVPaxos) isdead() bool {
	return atomic.LoadInt32(&kv.dead) != 0
}

// please do not change these two functions.
func (kv *KVPaxos) setunreliable(what bool) {
	if what {
		atomic.StoreInt32(&kv.unreliable, 1)
	} else {
		atomic.StoreInt32(&kv.unreliable, 0)
	}
}

func (kv *KVPaxos) isunreliable() bool {
	return atomic.LoadInt32(&kv.unreliable) != 0
}

//
// servers[] contains the ports of the set of
// servers that will cooperate via Paxos to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
//
func StartServer(servers []string, me int) *KVPaxos {
	// call gob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	gob.Register(Op{})

	kv := new(KVPaxos)
	kv.me = me

	// Your initialization code here.
	kv.key_value = make(map[string]string)
	kv.request = make(map[string]int)
	kv.seq = 0
	//

	rpcs := rpc.NewServer()
	rpcs.Register(kv)

	kv.px = paxos.Make(servers, me, rpcs)

	os.Remove(servers[me])
	l, e := net.Listen("unix", servers[me])
	if e != nil {
		log.Fatal("listen error: ", e)
	}
	kv.l = l


	// please do not change any of the following code,
	// or do anything to subvert it.

	go func() {
		for kv.isdead() == false {
			conn, err := kv.l.Accept()
			if err == nil && kv.isdead() == false {
				if kv.isunreliable() && (rand.Int63()%1000) < 100 {
					// discard the request.
					conn.Close()
				} else if kv.isunreliable() && (rand.Int63()%1000) < 200 {
					// process the request but force discard of reply.
					c1 := conn.(*net.UnixConn)
					f, _ := c1.File()
					err := syscall.Shutdown(int(f.Fd()), syscall.SHUT_WR)
					if err != nil {
						fmt.Printf("shutdown: %v\n", err)
					}
					go rpcs.ServeConn(conn)
				} else {
					go rpcs.ServeConn(conn)
				}
			} else if err == nil {
				conn.Close()
			}
			if err != nil && kv.isdead() == false {
				fmt.Printf("KVPaxos(%v) accept: %v\n", me, err.Error())
				kv.kill()
			}
		}
	}()

	return kv
}
