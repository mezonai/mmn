package config

const (
	Leader1 = "6a4dd9b6efe0fc8f125be331735b0e33239e24f02c84e555ade9ea50bd1369db"
	Leader2 = "650b1d76a0e9f0e57b44c4bf6342f7e4b511d9b1310f7898129c90abca296248"
	Leader3 = "066c7311f0f02057565bc1c4133ac3d009e9474ea90daa89ec9cf89d289d8e9b"
)

var LeaderSchedules = []LeaderSchedule{
	{StartSlot: 0, EndSlot: 99999, Leader: Leader1},
	{StartSlot: 100000, EndSlot: 199999, Leader: Leader2},
	{StartSlot: 200000, EndSlot: 299999, Leader: Leader3},
}
