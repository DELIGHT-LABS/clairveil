export function createValueMovingActionGate() {
  let active = null;
  let generation = 0;

  return Object.freeze({
    get active() {
      return Boolean(active);
    },

    get action() {
      return active?.action || "";
    },

    run(action, task) {
      if (active) return active.promise;
      if (typeof task !== "function") {
        return Promise.reject(new TypeError("value-moving action callback is required"));
      }

      const token = {
        action: String(action || "value-moving action"),
        generation: ++generation,
        promise: null
      };
      let resolvePromise;
      let rejectPromise;
      token.promise = new Promise((resolve, reject) => {
        resolvePromise = resolve;
        rejectPromise = reject;
      });
      active = token;

      let result;
      try {
        // Invoke synchronously after publishing the token. A second click in
        // the same event turn therefore observes this exact in-flight promise.
        result = task();
      } catch (error) {
        result = Promise.reject(error);
      }
      Promise.resolve(result).then(
        value => {
          if (active === token) active = null;
          resolvePromise(value);
        },
        error => {
          if (active === token) active = null;
          rejectPromise(error);
        }
      );
      return token.promise;
    },

    invalidate() {
      generation += 1;
      active = null;
    }
  });
}
