import crypto from "crypto"

if (typeof globalThis.crypto === 'undefined') {
    Object.defineProperty(globalThis, 'crypto', {
      value: {
        getRandomValues: function(buf) { return crypto.randomFillSync(buf);}
      },
      enumerable: false,
      configurable: true,
      writable: true,
    });
}