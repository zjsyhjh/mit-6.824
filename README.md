# MIT Distributed System Labs

### Lab1

- **Part I: word count**
    - 实现Map()和Reduce()函数，Map()返回包含Key/Value的list.List，Reduce()返回每个单词出现次数。
- **Part II: Distributing MapReduce jobs & Part III: Handling worker failures**
    - Example: test_test.go的TestBasic测试函数
    - setup() -> MakeMapReduce() -> InitMapReduce() -> StartRegistrationServer() -> Run() 注册RPC， 启动Master.

    - test_test.go文件注册worker并且启动Worker，worker等待从master发来的map以及reduce任务。
    - 修改MapReduce结构，新增空闲通道idleChannel, 每次选择完成map或者reduce人物的空闲worker。
    - RunMaster()函数的任务就是master通过rpc远程调用，发送map或者reduce给空闲的worker，需要注意的是reduce任务必须在所有map任务执行完成之后才能开始做。