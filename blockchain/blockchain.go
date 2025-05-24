package blockchain

type Blockchain struct {
	Blocks []*Block
}

type Transaction struct {
	Sender   string
	Receiver string
	Amount   float64
	Coinbase bool
}

func InitBlockChain() *Blockchain {
	return &Blockchain{[]*Block{Genesis()}}
}

func (chain *Blockchain) AddBlock(data string, coinbaseRcpt string, transactions []*Transaction) {
	prevBlock := chain.Blocks[len(chain.Blocks)-1]

	coinbaseTransaction := &Transaction{
		Sender:   "Coinbase",
		Receiver: coinbaseRcpt,
		Amount:   10.0,
		Coinbase: true,
	}

	newBlock := CreateBlock(data, prevBlock.Hash, append([]*Transaction{coinbaseTransaction}, transactions...))

	chain.Blocks = append(chain.Blocks, newBlock)
}

func (chain *Blockchain) ValidateChain() bool {
	for i := 1; i < len(chain.Blocks); i++ {
		currentBlock := chain.Blocks[i]
		prevBlock := chain.Blocks[i-1]

		if currentBlock.PrevHash != prevBlock.Hash {
			return false
		}

		pow := NewProofOfWork(currentBlock)
		if !pow.Validate() {
			return false
		}
	}
	return true
}
