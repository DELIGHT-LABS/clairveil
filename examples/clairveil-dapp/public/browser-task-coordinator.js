export function createBrowserTaskCoordinator({ locks = globalThis.navigator?.locks } = {}) {
  let tail = Promise.resolve();
  let pending = 0;
  let generation = 0;

  async function runLocked(lockName, task) {
    if (typeof locks?.request === "function") {
      return locks.request(lockName, { mode: "exclusive" }, task);
    }
    return task();
  }

  return Object.freeze({
    get pending() {
      return pending;
    },

    get generation() {
      return generation;
    },

    reset() {
      generation += 1;
      tail = Promise.resolve();
      pending = 0;
      return generation;
    },

    run(lockName, task) {
      if (typeof task !== "function") {
        return Promise.reject(new TypeError("coordinated task must be a function"));
      }
      const normalizedLockName = String(lockName || "").trim();
      if (!normalizedLockName) {
        return Promise.reject(new TypeError("coordinated task requires a lock name"));
      }

      const operationGeneration = generation;
      pending += 1;
      const operation = tail.then(
        () => runLocked(normalizedLockName, task),
        () => runLocked(normalizedLockName, task)
      );
      tail = operation.catch(() => {});
      return operation.finally(() => {
        if (operationGeneration === generation) pending -= 1;
      });
    }
  });
}
