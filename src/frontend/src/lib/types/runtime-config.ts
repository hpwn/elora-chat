export type RuntimeConfig = {
  schemaVersion: number;
  apiBaseUrl: string;
  wsUrl: string;
  allowedOrigins: string[];
  twitchChannel: string;
  youtubeSourceUrl: string;
  features: {
    showBadges: boolean;
    hideYouTubeAt: boolean;
    wsEnvelope: boolean;
    wsDropEmpty: boolean;
  };
  tailer: {
    enabled: boolean;
    pollIntervalMs: number;
    maxBatch: number;
    maxLagMs: number;
    persistOffsets: boolean;
    offsetPath: string;
  };
  websocket: {
    pingIntervalMs: number;
    pongWaitMs: number;
    writeDeadlineMs: number;
    maxMessageBytes: number;
  };
  ingest: {
    gnastyBin: string;
    gnastyArgs: string[];
    backoffBaseMs: number;
    backoffMaxMs: number;
  };
  gnasty: {
    sinks: {
      enabled: string[];
      batchSize: number;
      flushMaxMs: number;
    };
    twitch: {
      nick: string;
      tls: boolean;
      debugDrops: boolean;
      backoffMinMs: number;
      backoffMaxMs: number;
      refreshBackoffMinMs: number;
      refreshBackoffMaxMs: number;
    };
    youtube: {
      retrySeconds: number;
      dumpUnhandled: boolean;
      pollTimeoutSecs: number;
      pollIntervalMs: number;
      debug: boolean;
    };
  };
};

export type RuntimeConfigResponse = {
  config: RuntimeConfig;
  changed?: boolean;
  reconnectWs: boolean;
  restartRequired?: string[];
};
