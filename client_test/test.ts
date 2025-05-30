import axios from "axios";
import { ethers } from "ethers";
const wallet = new ethers.Wallet(// from address
  "xxx"
);
const toAddress = "0x35Ea12C82B9335f60db2AFE2D347EecD6B34C316";
const API_URL = "http://localhost:8081"
async function signTx() {
  // Sender    string  `json:"sender"`
	// Recipient string  `json:"recipient"`
	// Amount    big.Int `json:"amount"`
	// Timestamp big.Int `json:"timestamp"`
	// Nonce     big.Int `json:"nonce,omitempty"` // Optional nonce to prevent replay.
  const tx = {
    nonce: 0,
    to: toAddress,
    value: ethers.parseEther("1.0"),
  };

  const signedTx = await wallet.signTransaction(tx);
  console.log("Signed Tx:", signedTx);

  const response = await axios.post(`${API_URL}/sendRawTransaction`, {
      rawData: signedTx
    });
  console.log(`Response`, response.data);
  
}

signTx().catch(console.error);
