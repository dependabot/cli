const recordedTime = Number(process.env.DEPENDABOT_RECORDED_AT)
if (!Number.isFinite(recordedTime)) {
  throw new Error('Invalid Dependabot replay timestamp')
}

// Freeze wall-clock dates, not timers, performance.now(), or TLS verification.
const NativeDate = Date
NativeDate.now = () => recordedTime
globalThis.Date = new Proxy(NativeDate, {
  apply() {
    return new NativeDate(recordedTime).toString()
  },
  construct(target, args, newTarget) {
    return Reflect.construct(target, args.length ? args : [recordedTime], newTarget)
  },
})
NativeDate.prototype.constructor = globalThis.Date
