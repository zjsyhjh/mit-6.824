### Lab1：MapReduce

- Master如何与worker进行通信？

  - Master通过unix domain socket的方式与worker进行通信，master启动时，绑定`/var/tmp`目录下某个文件，注册master的RPC服务，然后启动一个goroutine，通过调用ServeConn在一个连接上处理请求，更典型地，可以创建一个network listener然后accept请求。

    - ```go
      func (mr *MapReduce) StartRegistrationServer() {
      	rpcs := rpc.NewServer()
      	rpcs.Register(mr)
      	os.Remove(mr.MasterAddress) // only needed for "unix"
      	l, e := net.Listen("unix", mr.MasterAddress)
      	if e != nil {
      		log.Fatal("RegstrationServer", mr.MasterAddress, " error: ", e)
      	}
      	mr.l = l
      	// now that we are listening on the master address, can fork off
      	// accepting connections to another thread.
      	go func() {
      		for mr.alive {
      			conn, err := mr.l.Accept()
      			if err == nil {
      				go func() {
      					rpcs.ServeConn(conn)
      					conn.Close()
      				}()
      			} else {
      				DPrintf("RegistrationServer: accept error", err)
      				break
      			}
      		}
      		DPrintf("RegistrationServer: done\n")
      	}()
      }
      ```

  - master注册完成之后，一边等待worker的连接，一边开始自己的作业（将文件切分成nMap份）

    - ```go
      func MakeMapReduce(nmap int, nreduce int,
      	file string, master string) *MapReduce {
      	mr := InitMapReduce(nmap, nreduce, file, master)
      	mr.StartRegistrationServer()
      	go mr.Run()
      	return mr
      }
      ```

    - ```go
      func (mr *MapReduce) Run() {
      	fmt.Printf("Run mapreduce job %s %s\n", mr.MasterAddress, mr.file)

      	mr.Split(mr.file)
      	mr.stats = mr.RunMaster()
      	mr.Merge()
      	mr.CleanupRegistration()
        
      	fmt.Printf("%s: MapReduce done\n", mr.MasterAddress)
      	mr.DoneChannel <- true
      }
      ```

  - Worker启动时，也绑定`/var/tmp`目录下的某个文件，注册worker的RPC服务，并且通过RPC调用master的Register函数，将自己的身份标识（如worker name) 发送给master的register channel，通知master此时worker已经成功启动，并且准备开始接收任务。

    - ```go
      func RunWorker(MasterAddress string, me string,
      	MapFunc func(string) *list.List,
      	ReduceFunc func(string, *list.List) string, nRPC int) {
      	DPrintf("RunWorker %s\n", me)
      	wk := new(Worker)
      	wk.name = me
      	wk.Map = MapFunc
      	wk.Reduce = ReduceFunc
      	wk.nRPC = nRPC
      	rpcs := rpc.NewServer()
      	rpcs.Register(wk)
      	os.Remove(me) // only needed for "unix"
      	l, e := net.Listen("unix", me)
      	if e != nil {
      		log.Fatal("RunWorker: worker ", me, " error: ", e)
      	}
      	wk.l = l
      	Register(MasterAddress, me)
      	// DON'T MODIFY CODE BELOW
      	for wk.nRPC != 0 {
      		conn, err := wk.l.Accept()
      		if err == nil {
      			wk.nRPC -= 1
      			go rpcs.ServeConn(conn)
      			wk.nJobs += 1
      		} else {
      			break
      		}
      	}
      	wk.l.Close()
      	DPrintf("RunWorker %s exit\n", me)
      }
      ```

    - ```go
      // Tell the master we exist and ready to work
      func Register(master string, me string) {
      	args := &RegisterArgs{}
      	args.Worker = me
      	var reply RegisterReply
      	ok := call(master, "MapReduce.Register", args, &reply)
      	if ok == false {
      		fmt.Printf("Register: RPC %s register error\n", master)
      	}
      }

      func (mr *MapReduce) Register(args *RegisterArgs, res *RegisterReply) error {
      	DPrintf("Register: worker %s\n", args.Worker)
      	mr.registerChannel <- args.Worker
      	res.OK = true
      	return nil
      }
      ```

  - Master一直在等待给worker分配任务，当它发现register channel存在数据了，说明已经有worker准备好了，可以给它分配任务了，通过RPC调用worker的DoJob函数，分配任务。

    - ```go
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
      ```

  - 由于Reduce任务必须等待Map任务完成之后才能开始，因此可以设置一个大小为nMap的mapChannel，每完成一次Map任务（Map任务的并行通过goroutine实现），就往mapChannel传送一次数据，然后就可以通过循环从mapChannel取数据的方式来等待所有的Map任务结束。并且当某个完成任务之后，还可以再次被分配，因此增加了idleChannel用来放置空闲的worker，当某个worker任务完成时，就放到idleChannel中，这样下次还可以被分配任务。Reduce任务部分的分配和Map类似。

  - Master通过RPC调用worker的DoJob函数，worker根据job内容来执行DoMap或者DoReduce函数

    - ```go
      // The master sent us a job
      func (wk *Worker) DoJob(arg *DoJobArgs, res *DoJobReply) error {
      	fmt.Printf("Dojob %s job %d file %s operation %v N %d\n",
      		wk.name, arg.JobNumber, arg.File, arg.Operation,
      		arg.NumOtherPhase)
      	switch arg.Operation {
      	case Map:
      		DoMap(arg.JobNumber, arg.File, arg.NumOtherPhase, wk.Map)
      	case Reduce:
      		DoReduce(arg.JobNumber, arg.File, arg.NumOtherPhase, wk.Reduce)
      	}
      	res.OK = true
      	return nil
      }
      ```

  - 以wordcount为例，DoMap将与当前Map任务相关联的文件内容平均切割成nReduce份，然后通过hash取模的方式将文件内的单词映射到nReduce份文件中（nMap个Map任务，nReduce个Reduce任务，一共有nMap * nReduce份文件），这样可以保证hash值相同的单词落在某个Reduce槽中，DoReduce就是对同一个槽中相同的单词进行统计计数。

    - ```go
      // Read split for job, call Map for that split, and create nreduce
      // partitions.
      func DoMap(JobNumber int, fileName string,
      	nreduce int, Map func(string) *list.List) {
      	name := MapName(fileName, JobNumber)
      	file, err := os.Open(name)
      	if err != nil {
      		log.Fatal("DoMap: ", err)
      	}
      	fi, err := file.Stat()
      	if err != nil {
      		log.Fatal("DoMap: ", err)
      	}
      	size := fi.Size()
      	fmt.Printf("DoMap: read split %s %d\n", name, size)
      	b := make([]byte, size)
      	_, err = file.Read(b)
      	if err != nil {
      		log.Fatal("DoMap: ", err)
      	}
      	file.Close()
      	res := Map(string(b))
      	// XXX a bit inefficient. could open r files and run over list once
      	for r := 0; r < nreduce; r++ {
      		file, err = os.Create(ReduceName(fileName, JobNumber, r))
      		if err != nil {
      			log.Fatal("DoMap: create ", err)
      		}
      		enc := json.NewEncoder(file)
      		for e := res.Front(); e != nil; e = e.Next() {
      			kv := e.Value.(KeyValue)
      			if ihash(kv.Key)%uint32(nreduce) == uint32(r) {
      				err := enc.Encode(&kv)
      				if err != nil {
      					log.Fatal("DoMap: marshall ", err)
      				}
      			}
      		}
      		file.Close()
      	}
      }
      ```

    - ```go
      // Read map outputs for partition job, sort them by key, call reduce for each
      // key
      func DoReduce(job int, fileName string, nmap int,
      	Reduce func(string, *list.List) string) {
      	kvs := make(map[string]*list.List)
      	for i := 0; i < nmap; i++ {
      		name := ReduceName(fileName, i, job)
      		fmt.Printf("DoReduce: read %s\n", name)
      		file, err := os.Open(name)
      		if err != nil {
      			log.Fatal("DoReduce: ", err)
      		}
      		dec := json.NewDecoder(file)
      		for {
      			var kv KeyValue
      			err = dec.Decode(&kv)
      			if err != nil {
      				break
      			}
      			_, ok := kvs[kv.Key]
      			if !ok {
      				kvs[kv.Key] = list.New()
      			}
      			kvs[kv.Key].PushBack(kv.Value)
      		}
      		file.Close()
      	}
      	var keys []string
      	for k := range kvs {
      		keys = append(keys, k)
      	}
      	sort.Strings(keys)
      	p := MergeName(fileName, job)
      	file, err := os.Create(p)
      	if err != nil {
      		log.Fatal("DoReduce: create ", err)
      	}
      	enc := json.NewEncoder(file)
      	for _, k := range keys {
      		res := Reduce(k, kvs[k])
      		enc.Encode(KeyValue{k, res})
      	}
      	file.Close()
      }
      ```

  - 最后对所有的Reduce槽结果进行合并排序，统计出现次数最多的单词。