package mapreduce

import "container/list"
import "fmt"

type WorkerInfo struct {
	address string
	// You can add definitions here.
}

// Clean up all workers by sending a Shutdown RPC to each one of them Collect
// the number of jobs each work has performed.
func (mr *MapReduce) KillWorkers() *list.List {
	l := list.New()
	for _, w := range mr.Workers {
		DPrintf("DoWork: shutdown %s\n", w.address)
		args := &ShutdownArgs{}
		var reply ShutdownReply
		ok := call(w.address, "Worker.Shutdown", args, &reply)
		if ok == false {
			fmt.Printf("DoWork: RPC %s shutdown error\n", w.address)
		} else {
			l.PushBack(reply.Njobs)
		}
	}
	return l
}

func (mr *MapReduce) RunMaster() *list.List {
	// Your code here

	var sendMapWork = func(worker string, jobNumber int) bool {
		args := &DoJobArgs{}
		args.File = mr.file
		args.JobNumber = jobNumber
		args.NumOtherPhase = mr.nReduce
		args.Operation = Map
		var reply DoJobReply
		ok := call(worker, "Worker.DoJob", args, &reply)
		return ok
	}

	var sendReduceWork = func(worker string, jobNumber int) bool {
		args := &DoJobArgs{}
		args.File = mr.file
		args.JobNumber = jobNumber
		args.NumOtherPhase = mr.nMap
		args.Operation = Reduce
		var reply DoJobReply
		ok := call(worker, "Worker.DoJob", args, &reply)
		return ok
	}

	mapChannel := make(chan int, mr.nMap)

	// Do map
	for i := 0; i < mr.nMap; i++ {
		go func(index int) {
			for {
				var ok bool = false
				var worker string

				select {
				case worker = <-mr.idleChannel:
					ok = sendMapWork(worker, index)

				case worker = <-mr.registerChannel:
					ok = sendMapWork(worker, index)

				}

				if ok {
					mapChannel <- index
					mr.idleChannel <- worker
					return
				}
			}
		}(i)
	}

	// Wait for map done
	for i := 0; i < mr.nMap; i++ {
		<-mapChannel
	}

	fmt.Println("Map is done.")

	// Do reduce
	reduceChannel := make(chan int, mr.nReduce)

	for i := 0; i < mr.nReduce; i++ {

		go func(index int) {
			for {
				var ok bool = false
				var worker string

				select {

				case worker = <-mr.idleChannel:
					ok = sendReduceWork(worker, index)

				case worker = <-mr.registerChannel:
					ok = sendReduceWork(worker, index)
				}
				if ok {
					reduceChannel <- index
					mr.idleChannel <- worker
					return
				}
			}
		}(i)

	}
	
	// Wait for reduce done
	for i := 0; i < mr.nReduce; i++ {
		<-reduceChannel
	}

	fmt.Println("Reduce is done.")

	return mr.KillWorkers()
}
