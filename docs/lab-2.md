### Lab2：Primary/Backup Key/Value Service

- 先介绍几个概念：

  - View：抽象概念，指系统的当前状态，server中谁是primary，谁是backup以及代表了历史进程的view编号（单调递增）
  - ViewServer：相当于系统状态的决策者，用来检测各个server（即代码中的clerk）的状态并且决定它们的角色（primary/backup）以及推进系统状态的演进（primary挂了，backup来充当等系统状况）
    - ViewServer存在单点失败
  - 系统运行方式：Clerk每隔一个PingInterval向ViewServer发送一个Ping，告知ViewServer自己还“活着”，同时ViewServer根据自己所能看到的系统当前状态，将Clerk标记为primary或backup或者不做任何处理，并且决定是否更新View，同时回复Ping它的Clerk最新的View。
    - 什么时候需要更新View？系统引入了一个机制（Primary Acknowledgement），即ViewServer向primary回复了当前最新的View(i)，在下一次Ping中，primary就会携带相应的信息证明已经知道了这个View(i)的存在，当ViewServer收到了Ping之后，就能够确认primary已经知道了这个View(i)的存在，从而能够决定是否需要根据当前的状态更新View，可以从以下几个方面来考虑是否需要更新View
      > - primary挂了
      > - backup挂了
      > - 当前View中只有primary，当一个idle server发来ping时，ViewServer会将这个idle server设为backup
    - ViewServer如何判断某个server是挂了之后重启的？
      - 当某个server挂了之后重启，它给ViewServer发送Ping时会额外携带一个参数0来表明它曾经挂过。

- 代码分析

  - 系统通过StartServer函数初始化ViewServer，并且注册ViewServer的RPC服务，采用unix domain socket的方式与clerk通信，等待clerk的连接（Ping）

    - ```go
      func StartServer(me string) *ViewServer {
      	vs := new(ViewServer)
      	vs.me = me
      	// Your vs.* initializations here.
      	vs.view = View{0, "", ""}
      	vs.primaryAck = 0
      	vs.backupAck = 0
      	vs.primaryTick = 0
      	vs.backupTick = 0
      	vs.currentTick = 0
      	// tell net/rpc about our RPC server and handlers.
      	rpcs := rpc.NewServer()
      	rpcs.Register(vs)
      	// prepare to receive connections from clients.
      	// change "unix" to "tcp" to use over a network.
      	os.Remove(vs.me) // only needed for "unix"
      	l, e := net.Listen("unix", vs.me)
      	if e != nil {
      		log.Fatal("listen error: ", e)
      	}
      	vs.l = l
      	// please don't change any of the following code,
      	// or do anything to subvert it.

      	// create a thread to accept RPC connections from clients.
      	go func() {
      		for vs.isdead() == false {
      			conn, err := vs.l.Accept()
      			if err == nil && vs.isdead() == false {
      				atomic.AddInt32(&vs.rpccount, 1)
      				go rpcs.ServeConn(conn)
      			} else if err == nil {
      				conn.Close()
      			}
      			if err != nil && vs.isdead() == false {
      				fmt.Printf("ViewServer(%v) accept: %v\n", me, err.Error())
      				vs.Kill()
      			}
      		}
      	}()
      	// create a thread to call tick() periodically.
      	go func() {
      		for vs.isdead() == false {
      			vs.tick()
      			time.Sleep(PingInterval)
      		}
      	}()
      	return vs
      }
      ```

  - clerk通过Ping(viewnum)远程调用向VeiwServer询问最新的View，当ViewServer收到Ping时，根据以下几种情况判断：

    ​

    > - 如果系统当前的primary还没有确定，并且ViewServer中存储的viewnum为0，则说明系统刚启动，那么将当前发送Ping的clerk设为primary，并且更新系统的viewnum（viewnum = viewnum + 1)
    >
    > - 如果当前的primary存在并且刚好就是发送ping的clerk，又可以分为两种情况
    >
    >   > - 如果此时clerk的viewnum为0，则说明是挂了之后又重启的，那么系统应该进入下一个View，如果存在backup，则将backup提升为primary
    >   > - 否则就告诉ViewServer，primary已经知道了这个View的存在
    >
    > - 如果当前的backup为空，并且primary已经知道了最新View的存在，那么直接设置clerk为backup，并且更新View
    >
    > - 如果当前的backup就是这个发送Ping的clerk，如果这个clerk是挂了之后重启的，并且Primary已经获知了最新的ViewServer的状态，那么更新View

    ​

    - ```go
      func (vs *ViewServer) Ping(args *PingArgs, reply *PingReply) error {

      	// Your code here.
      	Viewnum := args.Viewnum
      	ck := args.Me

      	vs.mu.Lock()

      	switch {

      	case vs.view.Primary == "" && vs.view.Viewnum == 0:
      		vs.view.Primary = ck
      		vs.view.Viewnum = Viewnum + 1
      		vs.primaryAck = 0
      		vs.primaryTick = vs.currentTick

      	case vs.view.Primary == ck:
      		if Viewnum == 0 {
      			vs.PromoteBackup()
      		} else {
      			vs.primaryAck = Viewnum
      			vs.primaryTick = vs.currentTick
      		}

      	case vs.view.Backup == "" && vs.view.Viewnum == vs.primaryAck:
      		vs.view.Backup = ck
      		vs.view.Viewnum++
      		vs.backupTick = vs.currentTick

      	case vs.view.Backup == ck:
      		if Viewnum == 0 && vs.view.Viewnum == vs.primaryAck {
      			vs.view.Backup = ck
      			vs.view.Viewnum++
      			vs.backupTick = vs.currentTick
      		} else if Viewnum != 0 {
      			vs.backupTick = vs.currentTick
      		}
      	}

      	reply.View = vs.view

      	vs.mu.Unlock()

      	return nil
      }
      ```

  - primary/backup server提供key/value服务：通过golang内置的map来为每一个"put"或"append"操作保证唯一性（每一个操作最多只能操作一次）

    - 通过tick函数每隔一定的周期ping一下ViewServer，用于获取当前最新的View，如果发现当前的primary就是自己，并且又有新的backup加入，并且不同于自己先前知道的backup，那么这时候primary需要和新加入的backup进行同步操作，调用SyncBackup()函数

    - ```go
      //
      // ping the viewserver periodically.
      // if view changed:
      //   transition to new view.
      //   manage transfer of state from primary to new backup.
      //
      func (pb *PBServer) tick() {

      	// Your code here.
      	pb.mu.Lock()
      	reply_view, err := pb.vs.Ping(pb.view.Viewnum)
      	if err == nil {
      		if pb.me == reply_view.Primary && reply_view.Backup != "" && reply_view.Backup != pb.view.Backup {
      			args := &SyncArgs{pb.key_value, pb.view.Primary}
      			var reply SyncReply
      			ok := call(reply_view.Backup, "PBServer.SyncBackup", args, &reply)
      			if !ok || reply.Err != OK {
      				log.Fatal("Error : Sync Backup failed\n")
      			}
      		}
      	}
      	pb.view = reply_view
      	defer pb.mu.Unlock()
      }
      ```

    - 在进行PutAppend操作时，如果当前View的backup不为空，则需要将操作进行转发，转发过程中可能遇到网络延迟或者backup挂了，需要等待一个Ping的时间来得到最新的view，然后根据情况重试

    - ```go
      func (pb *PBServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply) error {

      	//used for at-most-once
      	request_key := args.UniqueID
      	pb.rw_mutex.Lock()
      	request_value, ok := pb.request[request_key]
      	if !ok || request_value != 2 {
      		//primary deal with the request
      		if pb.me == pb.view.Primary {
      			if args.Key == "" {
      				reply.Err = ErrEmptyKey
      			}
      			pb.request[request_key] = 1
      			//transmit the request to the backup
      			args.ForwardName = pb.me
      			var backup_reply PutAppendReply
      			backup_ok := false
      			if pb.view.Backup != "" {
      				backup_ok = call(pb.view.Backup, "PBServer.PutAppend", args, &backup_reply)
      			} else {
      				backup_ok = true
      			}
      			//if backup return false, maybe the backup is alive but
      			//a network error occurred, or the backup is dead. so
      			//wait ping interval to get the latest view, and then retry
      			for !backup_ok {
      				time.Sleep(viewservice.PingInterval)
      				if pb.view.Primary != pb.me {
      					backup_ok = false
      					break
      				} else if pb.view.Backup != "" {
      					backup_ok = call(pb.view.Backup, "PBServer.PutAppend", args, &backup_reply)
      				} else {
      					backup_ok = true
      				}
      			}
      			if backup_ok {
      				if _, exist := pb.key_value[args.Key]; args.Op == "Append" && exist {
      					pb.key_value[args.Key] += args.Value
      				} else {
      					pb.key_value[args.Key] = args.Value
      				}
      				pb.request[request_key] = 2
      				reply.Err = OK
      			} else {
      				reply.Err = ErrWrongServer
      			}
      		} else if pb.me == pb.view.Backup && args.ForwardName == pb.view.Primary {
      			//backup deal with the request transmitted from primary
      			if _, exist := pb.key_value[args.Key]; args.Op == "Append" && exist {
      				pb.key_value[args.Key] += args.Value
      			} else {
      				pb.key_value[args.Key] = args.Value
      			}
      			pb.request[request_key] = 2
      			reply.Err = OK
      		} else {
      			//if the server isn't primary, reject the request
      			reply.Err = ErrWrongServer
      		}
      	} else {
      		reply.Err = ErrDuplicateKey
      	}
      	defer pb.rw_mutex.Unlock()
      	return nil
      }
      ```

