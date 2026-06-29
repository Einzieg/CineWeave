export type ApiEnvelope<TData = unknown, TMeta = unknown> = {
  requestId: string;
  data?: TData;
  meta?: TMeta;
};

export type ApiErrorEnvelope<TDetails = unknown> = {
  requestId: string;
  error: {
    code: string;
    message: string;
    details?: TDetails;
    retryable: boolean;
  };
};

