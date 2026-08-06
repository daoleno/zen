import { useEffect, useRef, useState } from "react";
import {
  createStreamingMarkdownFrameController,
  type StreamingMarkdownFrameController,
} from "./streamingMarkdownFrameController";

/**
 * Shared Android/iOS presentation owner for streaming MessageBody.
 * Settled (!streaming) bodies stay direct and exact from props.
 * Streaming bodies publish the newest accepted value at most once per frame.
 */

const sharedAnimationFrameScheduler = {
  request(callback: () => void) {
    return requestAnimationFrame(callback);
  },
  cancel(handle: number) {
    cancelAnimationFrame(handle);
  },
};

export function useStreamingMarkdownPresentation(
  value: string,
  streaming: boolean,
): string {
  const [published, setPublished] = useState(value);
  const controllerRef = useRef<StreamingMarkdownFrameController | null>(null);

  useEffect(() => {
    const controller = createStreamingMarkdownFrameController({
      scheduler: sharedAnimationFrameScheduler,
      onPublish: setPublished,
    });
    controllerRef.current = controller;
    return () => {
      controller.dispose();
      if (controllerRef.current === controller) {
        controllerRef.current = null;
      }
    };
  }, []);

  useEffect(() => {
    controllerRef.current?.accept(value, streaming);
  }, [streaming, value]);

  if (!streaming) {
    return value;
  }
  return published;
}
