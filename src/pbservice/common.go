package pbservice

const (
	OK              = "OK"
	ErrNoKey        = "ErrNoKey"
	ErrEmptyKey     = "ErrEmptyKey"
	ErrWrongServer  = "ErrWrongServer"
	ErrBackupFaild  = "ErrBackupFaild"
	ErrDuplicateKey = "ErrDuplicateKey"
)

type Err string

// Put or Append
type PutAppendArgs struct {
	Key   string
	Value string
	// You'll have to add definitions here.
	UniqueID    string //the unique id
	ForwardName string
	Op          string
}

type PutAppendReply struct {
	Err Err
}

type GetArgs struct {
	Key string
	// You'll have to add definitions here.
}

type GetReply struct {
	Err   Err
	Value string
}

// Your RPC definitions here.
type SyncArgs struct {
	KeyValue map[string]string
	Primary  string
}

type SyncReply struct {
	Err Err
}
