import { GrpcTransport } from '@protobuf-ts/grpc-transport';
import { ChannelCredentials } from '@grpc/grpc-js';
import {
  TxServiceClient,
  ITxServiceClient,
} from './generated/tx.client';
import {
  AccountServiceClient,
  IAccountServiceClient,
} from './generated/account.client';
import {
  TransactionStatusServiceClient,
  ITransactionStatusServiceClient,
} from './generated/transaction_status.client';
import type {
  TxMsg as GenTxMsg,
  SignedTxMsg as GenSignedTxMsg,
  TxResponse as GenTxResponse,
  AddTxResponse as GenAddTxResponse,
} from './generated/tx';
import type {
  GetAccountRequest as GenGetAccountRequest,
  GetAccountResponse as GenGetAccountResponse,
  GetTxHistoryRequest as GenGetTxHistoryRequest,
  GetTxHistoryResponse as GenGetTxHistoryResponse,
} from './generated/account';

export class GrpcClient {
  private transport: GrpcTransport;
  private txClient: ITxServiceClient;
  private accountClient: IAccountServiceClient;

  constructor(serverAddress: string) {
    this.transport = new GrpcTransport({
      host: serverAddress,
      channelCredentials: ChannelCredentials.createInsecure(),
    });
    this.txClient = new TxServiceClient(this.transport);
    this.accountClient = new AccountServiceClient(this.transport);
  }

  async broadcastTransaction(
    txMsg: {
      type: number; sender: string; recipient: string; amount: number; timestamp: number; text_data: string; nonce: number;
    },
    signature: string
  ): Promise<{ ok: boolean; error?: string }> {
    const genTx: GenTxMsg = {
      type: txMsg.type,
      sender: txMsg.sender,
      recipient: txMsg.recipient,
      amount: BigInt(txMsg.amount),
      timestamp: BigInt(txMsg.timestamp),
      textData: txMsg.text_data,
      nonce: BigInt(txMsg.nonce),
    };
    const req: GenSignedTxMsg = { txMsg: genTx, signature };

    const call = this.txClient.txBroadcast(req as any as GenSignedTxMsg);
    const res: GenTxResponse = await call.response;
    return { ok: res.ok, error: res.error };
  }

  async addTransaction(
    txMsg: {
      type: number; sender: string; recipient: string; amount: number; timestamp: number; text_data: string; nonce: number;
    },
    signature: string
  ): Promise<{ ok: boolean; tx_hash?: string; error?: string }> {
    const genTx: GenTxMsg = {
      type: txMsg.type,
      sender: txMsg.sender,
      recipient: txMsg.recipient,
      amount: BigInt(txMsg.amount),
      timestamp: BigInt(txMsg.timestamp),
      textData: txMsg.text_data,
      nonce: BigInt(txMsg.nonce),
    };
    const req: GenSignedTxMsg = { txMsg: genTx, signature };

    const call = this.txClient.addTx(req as any as GenSignedTxMsg);
    const res: GenAddTxResponse = await call.response;
    return { ok: res.ok, tx_hash: res.txHash, error: res.error };
  }

  async getAccount(address: string): Promise<{ address: string; balance: string; nonce: string }> {
    const req: GenGetAccountRequest = { address };
    const call = this.accountClient.getAccount(req);
    const res: GenGetAccountResponse = await call.response;
    return {
      address: res.address,
      balance: res.balance.toString(),
      nonce: res.nonce.toString(),
    };
  }

  async getTxHistory(
    address: string,
    limit: number,
    offset: number,
    filter: number
  ): Promise<{ total: number; txs: { sender: string; recipient: string; amount: string; nonce: string; timestamp: string; status: string }[] }> {
    const req: GenGetTxHistoryRequest = { address, limit, offset, filter };
    const call = this.accountClient.getTxHistory(req);
    const res: GenGetTxHistoryResponse = await call.response;
    return {
      total: res.total,
      txs: res.txs.map(tx => ({
        sender: tx.sender,
        recipient: tx.recipient,
        amount: tx.amount.toString(),
        nonce: tx.nonce.toString(),
        timestamp: tx.timestamp.toString(),
        status: ['PENDING','CONFIRMED','FINALIZED','FAILED'][tx.status] || 'PENDING',
      })),
    };
  }

  close(): void {
    this.transport.close();
  }
}
