### Lab3：Paxos-based Key/Value Service

- 理解Paxos算法：解决的问题是在一个可能发生消息延迟、丢失、重复等异常的分布式系统中，如何就某个值达成一致，保证不论发生任何异常，都不会破坏决议的一致性
  - 算法提出与证明

    - 将议员的角色分为proposers、acceptors和learners（允许身兼数职）
    - proposers提出提案，提案信息包括提案编号和提议的value
    - acceptor收到提案后可以接受（accept）提案，若提案获得多数acceptors的接受，则称该提案被批准
    - learners只能“学习”被批准的提案（acceptors只需将批准的消息发送给指定的一个learner，其他learners向它询问已经通过的决议）
  - 决议的提出与批获：通过一个决议分为两个阶段

    - prepare阶段：
      - proposer选择一个提案编号n并将prepare请求发送给acceptors中的一个多数派
      - acceptor收到prepare消息后，如果提案的编号大于它已经回复的所有prepare消息，则acceptor将自己上次接受的提案回复给proposer，并承诺不再回复小于n的提案
    - accept阶段：
      - 当一个proposer收到了多数acceptors对prepare的回复后，就进入获批阶段。它向回复prepare请求的acceptors发送`accept`请求，包括编号n和决定了value（编号最大的提案具有的value值，如果没有接受的value，则自己决定）
      - 在不违背自己向其他proposer的承诺的前提下，acceptor收到accept请求后即接受这个请求
    - 这个过程在任何时候中断都可以保证正确性。例如如果一个proposer发现已经有其他proposer提出了编号更高的提案，则有必要中断这个过程，因此为了优化，在上述的prepare过程中，如果一个acceptor发现存在一个编号更高的提案，则需要通知，提醒中断这次提案。
  - 决议发布阶段：
    - 一个显而易见的方法是当acceptor批准了一个value时，将这个消息发送给所有learner，但是这样消息量太大。
    - 假设没有拜占庭问题，那么learner可以通过别的learner来获取已经获批的决议，因此acceptor只需将获批的消息发送给某一个learner，其他learner向它询问已经获批的决议

- 活锁问题：当一个proposer发现存在编号更大的提案时将终止提案，这意味着提出一个编号更大的提案会终止之前的提案过程。如果两个proposer在这种情况下都转而提出一个编号更大的提案，就可能陷入活锁。这种情况下，需要选举出一个leader，仅允许leader提出提案。

- 伪代码：

  - ```c
    proposer(v):
      while not decided:
        choose n, unique and higher than any n seen so far
        send prepare(n) to all servers including self
        if prepare_ok(n_a, v_a) from majority:
          v' = v_a with highest n_a; choose own v otherwise
          send accept(n, v') to all
          if accept_ok(n) from majority:
            send decided(v') to all

    acceptor's state:
      n_p (highest prepare seen)
      n_a, v_a (highest accept seen)

    acceptor's prepare(n) handler:
      if n > n_p
        n_p = n
        reply prepare_ok(n_a, v_a)
      else
        reply prepare_reject

    acceptor's accept(n, v) handler:
      if n >= n_p
        n_p = n
        n_a = n
        v_a = v
        reply accept_ok(n)
      else
        reply accept_reject
    ```

  ​

- 基于Paxos的Key/Value服务
  - 判断at-most-once的思路和之前Lab2差不多，就是用一个map[string]int来判重，通过GetArgs以及PutAppendArgs结构体中增加一个唯一的Uid来区分
  - 关于同步的问题，每天Get或者PutAppend之前必须先同步所有server的状态，对于当前接受请求的server，如果当前提交的请求序号小于所能看到的最大序号，则说明公开server没有达到最新的状态，此时需要等待server与别的server进行同步，通过proposalInstance函数获得提交序号为seq的value，然后更新自身server的状态


​