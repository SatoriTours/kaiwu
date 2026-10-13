export class APIError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}
export async function api<T>(path: string, body?: unknown): Promise<T> {
  let response: Response;
  try {
    response = await fetch("/api/" + path, {
      method: body === undefined ? "GET" : "POST",
      credentials: "same-origin",
      headers:
        body === undefined
          ? {}
          : { "Content-Type": "application/json", "X-Kaiwu-Client": "web" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new APIError(
      0,
      "offline",
      "连接中断，输入已保留；请恢复连接后确认操作结果",
    );
  }
  let data: any;
  try {
    data = await response.json();
  } catch {
    throw new APIError(
      0,
      "unknown_result",
      "响应未完整收到，请恢复连接后确认操作结果",
    );
  }
  if (!response.ok)
    throw new APIError(
      response.status,
      data.error?.code ?? "error",
      data.error?.message ?? "请求失败",
    );
  return data as T;
}
