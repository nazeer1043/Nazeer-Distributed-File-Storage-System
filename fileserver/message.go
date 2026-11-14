package fileserver

// Message is a struct that contains the payload of the message.
type Message struct {
	Payload any
}

// MessageStoreFile is a struct that contains the key and the size of the file.
type MessageStoreFile struct {
	ID       string
	Key      string
	Size     int64
	StreamID uint64
}

// MessageGetFile is a struct that contains the key of the file.
type MessageGetFile struct {
	Key      string
	ID       string
	StreamID uint64
}

// MessageDeleteFile is a struct that contains the key of the file to delete across peers.
type MessageDeleteFile struct {
	Key string
	ID  string
}

// MessageAck is an acknowledgement message for replication and quorum consensus.
type MessageAck struct {
	Key      string
	From     string
	Success  bool
	StreamID uint64
}
