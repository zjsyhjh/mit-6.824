package shardmaster

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
	"math"
)

type ShardMaster struct {
	mu         sync.Mutex
	l          net.Listener
	me         int
	dead       int32 // for testing
	unreliable int32 // for testing
	px         *paxos.Paxos

	configs []Config // indexed by config num
	processid int
	cfnum int
}


type Op struct {
	// Your data here.
	Type string //"Join", "Leave", "Move", "Query"
	GID int64
	Servers []string
	Shard int
	Num int
	Uid int64
}


//add new function
//proposal a new instance
func (sm *ShardMaster) proposalInstance(seq int, v interface{}) Op {
	sm.px.Start(seq, v)
	to := time.Millisecond * 10
	for {
		status, value := sm.px.Status(seq)
		if status == paxos.Decided {
			return value.(Op)
		}
		time.Sleep(to)
		if to < time.Second * 10 {
			to *= 2
		}
	}
	return Op{}
}

//get the index of maximum config.Groups
func (sm *ShardMaster) getMaxGidCounts(config *Config) int64 {
	counts := make(map[int64]int)
	max_gid, max_count := int64(0), -1
	for gid := range config.Groups {
		counts[gid] = 0
	}
	for _, gid := range config.Shards {
		counts[gid]++
	}

	for gid := range counts {
		if _, exist := config.Groups[gid]; exist {
			if counts[gid] > max_count {
				max_count, max_gid = counts[gid], gid
			}
		}
	}
	for _, gid := range config.Shards {
		if gid == 0 {
			max_gid = 0
			break
		}
	}
	return max_gid
}

//get the index of minimum config.Groups
func (sm *ShardMaster) getMinGidCounts(config *Config) int64 {
	counts := make(map[int64]int)
	min_gid, min_count := int64(0), math.MaxInt32
	for gid := range config.Groups {
		counts[gid] = 0
	}
	for _, gid := range config.Shards {
		counts[gid]++
	}

	for gid := range counts {
		if _, exist := config.Groups[gid]; exist {
			if counts[gid] < min_count {
				min_count, min_gid = counts[gid], gid
			}
		}
	}
	return min_gid
}


//return shard_id if the Shard[index] equals to the parameter(gid)
func (sm *ShardMaster) getShardByGid(gid int64, config *Config) int {
	for sid, g := range config.Shards {
		if g == gid {
			return sid
		}
	}
	return -1
}


//add new function
//rebalance load
func (sm *ShardMaster) rebalanceAfterJoin(gid int64) {
	config := &sm.configs[sm.cfnum]
	count := 0
	for {
		if NShards / len(config.Groups) == count {
			break
		}
		max_gid := sm.getMaxGidCounts(config)
		sid := sm.getShardByGid(max_gid, config)
		config.Shards[sid] = gid

		count += 1
	}
}

func (sm *ShardMaster) rebalanceAfterLeave(gid int64) {
	config := &sm.configs[sm.cfnum]
	for {
		min_gid := sm.getMinGidCounts(config)
		sid := sm.getShardByGid(gid, config)
		if sid == -1 {
			break
		}
		config.Shards[sid] = min_gid
	}
}

//add new function
func (sm *ShardMaster) nextConfig() *Config {
	old := &sm.configs[sm.cfnum]
	var config Config
	config.Num = old.Num + 1
	config.Shards = [NShards]int64{}
	config.Groups = make(map[int64][]string)
	for gid, servers := range old.Groups {
		config.Groups[gid] = servers
	}
	for index, gid := range old.Shards {
		config.Shards[index] = gid
	}
	sm.configs = append(sm.configs, config)
	sm.cfnum += 1
	return &sm.configs[sm.cfnum]
}

func (sm *ShardMaster) applyJoin(gid int64, servers []string) {
	config := sm.nextConfig()
	if _, exist := config.Groups[gid]; !exist {
		config.Groups[gid] = servers
		sm.rebalanceAfterJoin(gid)
	}
}

func (sm *ShardMaster) applyLeave(gid int64) {
	config := sm.nextConfig()
	if _, exist := config.Groups[gid]; exist {
		delete(config.Groups, gid)
		sm.rebalanceAfterLeave(gid)
	}
}

func (sm *ShardMaster) applyMove(gid int64, sid int) {
	config := sm.nextConfig()
	config.Shards[sid] = gid
}

func (sm *ShardMaster) applyQuery(num int) Config {
	if num == -1 {
		sm.checkValid(sm.configs[sm.cfnum])
		return sm.configs[sm.cfnum]
	} else {
		return sm.configs[num]
	}
}

func (sm *ShardMaster) checkValid(config Config) {
	if len(config.Groups) > 0 {
		for _, gid := range config.Shards {
			if _, ok := config.Groups[gid]; !ok {
				fmt.Println("Unallocated Shard")
				os.Exit(-1)
			}
		}
	}
}

//add apply function
func (sm *ShardMaster) apply(seq int, op Op) Config {
	sm.processid += 1
	switch op.Type {
	case "Join":
		sm.applyJoin(op.GID, op.Servers)
	case "Leave":
		sm.applyLeave(op.GID)
	case "Move":
		sm.applyMove(op.GID, op.Shard)
	case "Query":
		return sm.applyQuery(op.Num)
	default:
		fmt.Println("No such operate type.")
	}
	sm.px.Done(sm.processid)
	return Config{}
}

//add new function
//process the operation from client
func (sm *ShardMaster) process(op Op) Config {
	op.Uid = nrand()
	for {
		curSeq := sm.processid + 1
		v := sm.proposalInstance(curSeq, op)
		config := sm.apply(curSeq, v)
		if v.Uid == op.Uid {
			return config
		}
	}
}



func (sm *ShardMaster) Join(args *JoinArgs, reply *JoinReply) error {
	// Your code here.
	sm.mu.Lock()
	defer sm.mu.Unlock()
	op := Op{Type:"Join", GID:args.GID, Servers:args.Servers}
	sm.process(op)
	return nil
}

func (sm *ShardMaster) Leave(args *LeaveArgs, reply *LeaveReply) error {
	// Your code here.
	sm.mu.Lock()
	defer sm.mu.Unlock()
	op := Op{Type:"Leave", GID:args.GID}
	sm.process(op)
	return nil
}

func (sm *ShardMaster) Move(args *MoveArgs, reply *MoveReply) error {
	// Your code here.
	sm.mu.Lock()
	defer sm.mu.Unlock()
	op := Op{Type:"Move", GID:args.GID, Shard:args.Shard}
	sm.process(op)
	return nil
}

func (sm *ShardMaster) Query(args *QueryArgs, reply *QueryReply) error {
	// Your code here.
	sm.mu.Lock()
	defer sm.mu.Unlock()
	op := Op{Type:"Query", Num:args.Num}
	reply.Config = sm.process(op)
	return nil
}



// please don't change these two functions.
func (sm *ShardMaster) Kill() {
	atomic.StoreInt32(&sm.dead, 1)
	sm.l.Close()
	sm.px.Kill()
}

// call this to find out if the server is dead.
func (sm *ShardMaster) isdead() bool {
	return atomic.LoadInt32(&sm.dead) != 0
}

// please do not change these two functions.
func (sm *ShardMaster) setunreliable(what bool) {
	if what {
		atomic.StoreInt32(&sm.unreliable, 1)
	} else {
		atomic.StoreInt32(&sm.unreliable, 0)
	}
}

func (sm *ShardMaster) isunreliable() bool {
	return atomic.LoadInt32(&sm.unreliable) != 0
}

//
// servers[] contains the ports of the set of
// servers that will cooperate via Paxos to
// form the fault-tolerant shardmaster service.
// me is the index of the current server in servers[].
//
func StartServer(servers []string, me int) *ShardMaster {
	sm := new(ShardMaster)
	sm.me = me

	sm.configs = make([]Config, 1)
	sm.configs[0].Groups = make(map[int64][]string)
	sm.cfnum = 0
	sm.processid = 0

	rpcs := rpc.NewServer()

	gob.Register(Op{})
	rpcs.Register(sm)
	sm.px = paxos.Make(servers, me, rpcs)

	os.Remove(servers[me])
	l, e := net.Listen("unix", servers[me])
	if e != nil {
		log.Fatal("listen error: ", e)
	}
	sm.l = l

	// please do not change any of the following code,
	// or do anything to subvert it.

	go func() {
		for sm.isdead() == false {
			conn, err := sm.l.Accept()
			if err == nil && sm.isdead() == false {
				if sm.isunreliable() && (rand.Int63()%1000) < 100 {
					// discard the request.
					conn.Close()
				} else if sm.isunreliable() && (rand.Int63()%1000) < 200 {
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
			if err != nil && sm.isdead() == false {
				fmt.Printf("ShardMaster(%v) accept: %v\n", me, err.Error())
				sm.Kill()
			}
		}
	}()

	return sm
}
