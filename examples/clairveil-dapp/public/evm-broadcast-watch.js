const staleWatcherCode = "STALE_EVM_BROADCAST_WATCHER";

function staleWatcherError() {
  const error = new Error("EVM broadcast watcher was replaced by a newer action");
  error.code = staleWatcherCode;
  return error;
}

function isStaleWatcherError(error) {
  return error?.code === staleWatcherCode;
}

export function createEvmBroadcastWatcher({ reportError = () => {} } = {}) {
  const active = new Map();
  let generation = 0;

  function invalidateAll() {
    generation += 1;
    active.clear();
  }

  function watch(broadcast, {
    key,
    isCurrent,
    onIncluded,
    onUnknown,
    onFailed,
  } = {}) {
    if (!broadcast?.waitPromise) return null;
    const watchKey = String(key || "").trim();
    if (!watchKey) throw new Error("EVM broadcast watcher key is required");
    if (isCurrent != null && typeof isCurrent !== "function") {
      throw new TypeError("EVM broadcast watcher isCurrent must be a function");
    }

    const token = Object.freeze({ key: watchKey, generation: ++generation });
    active.set(watchKey, token);

    const tokenIsActive = () => active.get(watchKey) === token;
    const assertActive = () => {
      if (!tokenIsActive()) throw staleWatcherError();
    };
    const finish = () => {
      if (tokenIsActive()) active.delete(watchKey);
    };
    const evidenceIsCurrent = async evidence => {
      if (!tokenIsActive()) return false;
      if (!isCurrent) return true;
      try {
        return Boolean(await isCurrent(evidence));
      } catch (error) {
        if (!isStaleWatcherError(error)) reportError(error);
        return false;
      }
    };
    const handleFailure = async (error, evidence) => {
      if (isStaleWatcherError(error) || !await evidenceIsCurrent(evidence)) {
        finish();
        return;
      }
      try {
        await onFailed?.(error, assertActive);
      } catch (callbackError) {
        if (!isStaleWatcherError(callbackError)) reportError(callbackError);
      } finally {
        finish();
      }
    };

    void Promise.resolve(broadcast.waitPromise).then(async result => {
      if (!await evidenceIsCurrent(result)) {
        finish();
        return;
      }
      try {
        const callback = result?.unknown ? onUnknown : onIncluded;
        await callback?.(result, assertActive);
        finish();
      } catch (error) {
        await handleFailure(error, result);
      }
    }, error => handleFailure(error, error)).catch(error => {
      if (!isStaleWatcherError(error)) reportError(error);
      finish();
    });
    return token;
  }

  return Object.freeze({ invalidateAll, watch });
}
