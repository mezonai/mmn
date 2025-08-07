import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import * as path from 'path';

// Load proto files from local client_test/proto directory
const PROTO_PATH = path.join(__dirname, 'proto');

const txPackageDefinition = protoLoader.loadSync(
  path.join(PROTO_PATH, 'tx.proto'),
  {
    keepCase: true,
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true
  }
);

const accountPackageDefinition = protoLoader.loadSync(
  path.join(PROTO_PATH, 'account.proto'),
  {
    keepCase: true,
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true
  }
);

// Define proper TypeScript interfaces based on proto definitions
export interface TxMsg {
  type: number;
  sender: string;
  recipient: string;
  amount: number;
  timestamp: number;
  text_data: string;
  nonce: number;
}

export interface SignedTxMsg {
  tx_msg: TxMsg;
  signature: string;
}

export interface TxResponse {
  ok: boolean;
  error?: string;
}

export interface AddTxResponse {
  ok: boolean;
  tx_hash?: string;
  error?: string;
}

export interface GetAccountRequest {
  address: string;
}

export interface GetAccountResponse {
  address: string;
  balance: string; // uint64 comes as string due to longs: String
  nonce: string;   // uint64 comes as string due to longs: String
}

export interface GetTxHistoryRequest {
  address: string;
  limit: number;
  offset: number;
  filter: number;
}

export interface TxMeta {
  sender: string;
  recipient: string;
  amount: string; // uint64 comes as string due to longs: String
  nonce: string;  // uint64 comes as string due to longs: String
  timestamp: string; // uint64 comes as string due to longs: String
  status: string; // enum comes as string due to enums: String
}

export interface GetTxHistoryResponse {
  total: number;
  txs: TxMeta[];
}

// Define gRPC client types
interface TxServiceClient {
  TxBroadcast: (request: SignedTxMsg, callback: (error: grpc.ServiceError | null, response: TxResponse) => void) => void;
  AddTx: (request: SignedTxMsg, callback: (error: grpc.ServiceError | null, response: AddTxResponse) => void) => void;
}

interface AccountServiceClient {
  GetAccount: (request: GetAccountRequest, callback: (error: grpc.ServiceError | null, response: GetAccountResponse) => void) => void;
  GetTxHistory: (request: GetTxHistoryRequest, callback: (error: grpc.ServiceError | null, response: GetTxHistoryResponse) => void) => void;
}

const txProto = grpc.loadPackageDefinition(txPackageDefinition) as unknown as { mmn: { TxService: new (address: string, credentials: grpc.ChannelCredentials) => TxServiceClient } };
const accountProto = grpc.loadPackageDefinition(accountPackageDefinition) as unknown as { mmn: { AccountService: new (address: string, credentials: grpc.ChannelCredentials) => AccountServiceClient } };

export class GrpcClient {
  private txClient: TxServiceClient;
  private accountClient: AccountServiceClient;

  constructor(serverAddress: string) {
    this.txClient = new txProto.mmn.TxService(serverAddress, grpc.credentials.createInsecure());
    this.accountClient = new accountProto.mmn.AccountService(serverAddress, grpc.credentials.createInsecure());
  }

  async broadcastTransaction(txMsg: TxMsg, signature: string): Promise<{ ok: boolean; error?: string }> {
    return new Promise((resolve, reject) => {
      const signedTx: SignedTxMsg = {
        tx_msg: txMsg,
        signature: signature
      };

      this.txClient.TxBroadcast(signedTx, (err: grpc.ServiceError | null, response: TxResponse) => {
        if (err) {
          reject(err);
        } else {
          resolve({ ok: response.ok, error: response.error });
        }
      });
    });
  }

  async addTransaction(txMsg: TxMsg, signature: string): Promise<{ ok: boolean; tx_hash?: string; error?: string }> {
    return new Promise((resolve, reject) => {
      const signedTx: SignedTxMsg = {
        tx_msg: txMsg,
        signature: signature
      };

      this.txClient.AddTx(signedTx, (err: grpc.ServiceError | null, response: AddTxResponse) => {
        if (err) {
          reject(err);
        } else {
          resolve({ ok: response.ok, tx_hash: response.tx_hash, error: response.error });
        }
      });
    });
  }

  async getAccount(address: string): Promise<{ address: string; balance: string; nonce: string }> {
    return new Promise((resolve, reject) => {
      this.accountClient.GetAccount({ address: address }, (err: grpc.ServiceError | null, response: GetAccountResponse) => {
        if (err) {
          reject(err);
        } else {
          resolve({
            address: response.address,
            balance: response.balance,
            nonce: response.nonce
          });
        }
      });
    });
  }

  async getTxHistory(
    address: string,
    limit: number,
    offset: number,
    filter: number
  ): Promise<{ total: number; txs: TxMeta[] }> {
    return new Promise((resolve, reject) => {
      this.accountClient.GetTxHistory(
        { address, limit, offset, filter },
        (err: grpc.ServiceError | null, response: GetTxHistoryResponse) => {
          if (err) {
            reject(err);
          } else {
            resolve({
              total: response.total,
              txs: response.txs
            });
          }
        }
      );
    });
  }

  close(): void {
    this.txClient = null as any;
    this.accountClient = null as any;
  }
}
