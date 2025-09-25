import axios, { AxiosInstance } from 'axios';
import { ZkClientConfig, IZkProof } from './types';

export class ZkClient {
  private axiosInstance: AxiosInstance;
  private endpoint: string;

  constructor(config: ZkClientConfig) {
    this.endpoint = config.endpoint;

    this.axiosInstance = axios.create({
      baseURL: this.endpoint,
      timeout: config.timeout || 30000,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...(config.headers || {}),
      },
    });
  }

  private async makeRequest<T>(
    method: 'GET' | 'POST',
    path: string,
    params?: Record<string, string | number>,
    body?: any
  ): Promise<T> {
    const resp = await this.axiosInstance.request<T>({
      method,
      url: path,
      params,
      data: body,
    });
    return resp.data;
  }

  public async getZkProofs({
    userId,
    ephemeralPublicKey,
    jwt,
    address,
  }: {
    userId: string;
    ephemeralPublicKey: string;
    jwt: string;
    address: string;
  }): Promise<IZkProof> {
    const path = `prove`;
    const res = await this.makeRequest<{ data: IZkProof }>(
      'POST',
      path,
      undefined,
      {
        user_id: userId,
        ephemeral_pk: ephemeralPublicKey,
        jwt,
        address,
      }
    );

    return res.data;
  }
}
