export const TERMINAL_RENDERER_READY_TIMEOUT_MS = 4_000;
export const TERMINAL_RENDERER_MAX_AUTO_RETRIES = 1;

export interface TerminalFontAssetLike {
  uri?: string | null;
  localUri?: string | null;
  downloadAsync?: () => Promise<TerminalFontAssetLike>;
}

export type TerminalFontDiagnostic = (
  message: string,
  error?: unknown,
) => void;

export interface TerminalFontResolution {
  uri: string | null;
  source: 'cache' | 'asset' | 'fallback';
  backgroundLoad: Promise<void>;
}

function usableFontUri(value: string | null | undefined): string | null {
  if (typeof value !== 'string') {
    return null;
  }
  const trimmed = value.trim();
  return trimmed || null;
}

/**
 * Process-local font cache with a synchronous usable result.
 *
 * A remote/bundled Asset URI is enough to mount the WebView immediately.
 * Local download only warms later remounts and can never gate the renderer.
 */
export class TerminalFontCache {
  private cachedUri: string | null = null;

  resolve(
    loadAsset: () => TerminalFontAssetLike,
    report: TerminalFontDiagnostic,
  ): TerminalFontResolution {
    if (this.cachedUri) {
      return {
        uri: this.cachedUri,
        source: 'cache',
        backgroundLoad: Promise.resolve(),
      };
    }

    let asset: TerminalFontAssetLike;
    try {
      asset = loadAsset();
    } catch (error) {
      report('Bundled terminal font could not be resolved; using monospace fallback.', error);
      return {
        uri: null,
        source: 'fallback',
        backgroundLoad: Promise.resolve(),
      };
    }

    const localUri = usableFontUri(asset.localUri);
    const immediateUri = localUri ?? usableFontUri(asset.uri);
    if (immediateUri) {
      this.cachedUri = immediateUri;
    } else {
      report('Bundled terminal font has no usable URI; using monospace fallback.');
    }

    let backgroundLoad = Promise.resolve();
    if (!localUri && typeof asset.downloadAsync === 'function') {
      backgroundLoad = Promise.resolve()
        .then(() => asset.downloadAsync!())
        .then((downloaded) => {
          const downloadedUri = usableFontUri(downloaded.localUri) ??
            usableFontUri(downloaded.uri);
          if (downloadedUri) {
            this.cachedUri = downloadedUri;
            return;
          }
          report('Bundled terminal font download produced no usable URI; retaining fallback.');
        })
        .catch((error) => {
          report(
            immediateUri
              ? 'Bundled terminal font download failed; retaining the immediate URI.'
              : 'Bundled terminal font download failed; using monospace fallback.',
            error,
          );
        });
    }

    return {
      uri: immediateUri,
      source: immediateUri ? 'asset' : 'fallback',
      backgroundLoad,
    };
  }
}

export const terminalFontCache = new TerminalFontCache();

export type TerminalRendererBootstrapStatus = 'loading' | 'ready' | 'failed';

export interface TerminalRendererBootstrapState {
  generation: number;
  autoRetries: number;
  status: TerminalRendererBootstrapStatus;
  error: string | null;
}

export function isCurrentTerminalRendererGeneration(
  currentGeneration: number,
  incomingGeneration: unknown,
): incomingGeneration is number {
  return typeof incomingGeneration === 'number' &&
    Number.isSafeInteger(incomingGeneration) &&
    incomingGeneration === currentGeneration;
}

export type TerminalRendererBootstrapAction =
  | { type: 'load-start'; generation: number }
  | { type: 'ready'; generation: number }
  | { type: 'timeout'; generation: number; message: string }
  | { type: 'failure'; generation: number; message: string }
  | { type: 'retry' };

export function createTerminalRendererBootstrapState(): TerminalRendererBootstrapState {
  return {
    generation: 0,
    autoRetries: 0,
    status: 'loading',
    error: null,
  };
}

function rendererFailure(
  state: TerminalRendererBootstrapState,
  generation: number,
  message: string,
): TerminalRendererBootstrapState {
  if (
    !isCurrentTerminalRendererGeneration(state.generation, generation) ||
    state.status === 'failed'
  ) {
    return state;
  }
  if (state.autoRetries < TERMINAL_RENDERER_MAX_AUTO_RETRIES) {
    return {
      generation: state.generation + 1,
      autoRetries: state.autoRetries + 1,
      status: 'loading',
      error: message,
    };
  }
  return {
    ...state,
    status: 'failed',
    error: message,
  };
}

export function reduceTerminalRendererBootstrap(
  state: TerminalRendererBootstrapState,
  action: TerminalRendererBootstrapAction,
): TerminalRendererBootstrapState {
  switch (action.type) {
    case 'load-start':
      if (
        !isCurrentTerminalRendererGeneration(state.generation, action.generation) ||
        state.status === 'failed'
      ) {
        return state;
      }
      return state.status === 'loading' && state.error === null
        ? state
        : { ...state, status: 'loading', error: null };
    case 'ready':
      if (
        !isCurrentTerminalRendererGeneration(state.generation, action.generation) ||
        state.status !== 'loading'
      ) {
        return state;
      }
      return { ...state, status: 'ready', error: null };
    case 'failure':
    case 'timeout':
      return rendererFailure(state, action.generation, action.message);
    case 'retry':
      return {
        generation: state.generation + 1,
        autoRetries: 0,
        status: 'loading',
        error: null,
      };
  }
}

export function terminalRendererPresentation(
  state: TerminalRendererBootstrapState,
): 'loading' | 'ready' | 'failure' {
  return state.status === 'failed' ? 'failure' : state.status;
}
