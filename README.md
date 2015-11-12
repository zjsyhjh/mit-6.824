# MIT Distributed System Labs

### Lab1: MapReduce

- **Part I: word count**
    - 实现Map()和Reduce()函数，Map()返回包含Key/Value的list.List，Reduce()返回每个单词出现次数。
- **Part II: Distributing MapReduce jobs & Part III: Handling worker failures**
    - Example: test_test.go的TestBasic测试函数
    - setup() -> MakeMapReduce() -> InitMapReduce() -> StartRegistrationServer() -> Run() 注册RPC， 启动Master.

    - test_test.go文件注册worker并且启动Worker，worker等待从master发来的map以及reduce任务。
    - 修改MapReduce结构，新增空闲通道idleChannel, 每次选择完成map或者reduce人物的空闲worker。
    - RunMaster()函数的任务就是master通过rpc远程调用，发送map或者reduce给空闲的worker，需要注意的是reduce任务必须在所有map任务执行完成之后才能开始做。


### Lab2: Primary/Backup Key/Value Service
- **Part A: The Viewservice**
    - ViewService: 作为Master服务器（存在单点失败）， 用于检测各个Clerk（即实验中提到的server，这里充当client）的状态并且决定他们的角色（primary/backup）。
    - View: 抽象概念，指的是当前场景的状态（谁是primary/backup以及当前的view编号）。
    - 系统运行方式：Clerk每隔一个PingInterval向ViewServer发送一个Ping，告知server自己“活着”，同时server根据自己当前的状态，将Clerk标记为primary/backup或者不做任何处理，并且决定是否需要更新View，同时也回复Ping它的Clerk更新（?)之后的View。
    - 什么时候需要更新View: 首先需要引入一个机制（Primary Acknowledgement），即Master向Primary回复了当前最新的View(i)，在下一个Ping中，Primary就会携带响应信息证明已经知道了这个View(i)的存在，当Master收到Ping之后，就能够确认Primary已经知道这个View(i)的存在，从而能够决定是否需要根据当前的状态更新View，可以从以下几个方面来考虑是否需要更新View
        - Primary挂了。
        - Backup挂了。
        - 当前View中只有Primary，当一个idle server发来ping时，Master会将这个idle server设为Backup。
    - server挂了之后又重启，Master如何得知：*When a server re-starts after a crash, it should send one or more Pings with an argument of zero to inform the view service that it crashed*
    - 修改viewservice/server.go，新增几个状态变量，用于确定Primary/Backup是否Ack过View，以及Ping是否超时。
    
- **Part B: The primary/backup key/value service**
    - 可以用go内置类型map来保存"put"/"append"操作，但由于每个操作最多只能操作一下，因此还需要另外一个map来过滤同一个客户端提交的相同的操作；
    - tick()函数每隔一定的周期ping一下viewservice，用于获取当前最新的view，如果发现当前的primary就是自己并且又有新的backup加入而且与自己先前知道的backup不同，那么这时候primary就需要和新加入的backup进行同步操作，调用SyncBackup()函数。
    - 在PutAppend操作时，如果当前view的backup不为空，则需要将操作进行转发，转发过程中可能遇到网络延迟或者backup宕机，为了确保能够成功转发，需要一定的策略来判断。
    