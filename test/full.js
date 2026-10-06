#!/usr/bin/env node
'use strict'

// LX Music MCP server 全面功能测试。
//
// 相比 test/e2e.js 的"冒烟"定位，这个脚本逐条覆盖 9 个工具的每个分支、
// 错误路径，以及可靠性（重复调用、并发调用）。
//
// 用法：
//   node test/full.js [exe路径]
//
// 前提：LX Music 正在运行且已开启 Open API。
// 副作用：会创建/删除名为 __MCP-FULL-TEST__ 的歌单，并临时改变播放状态；
//         脚本结束前会尽量恢复音量、静音与当前播放的歌曲。
// 退出码：0 = 全部通过。

const { spawn } = require('node:child_process')
const path = require('node:path')
const fs = require('node:fs')

const exe = process.argv[2] || path.join(__dirname, '..', 'mcp', 'lx-music-mcp.exe')
const TEST_LIST = '__MCP-FULL-TEST__'
const CALL_TIMEOUT_MS = 90000

if (!fs.existsSync(exe)) {
  console.error('找不到可执行文件：' + exe)
  process.exit(1)
}

let pass = 0
let fail = 0
const failures = []

function ok (name, cond, extra) {
  if (cond) {
    pass++
    console.log('  [ok]   ' + name + (extra ? '  ' + extra : ''))
  } else {
    fail++
    failures.push(name)
    console.log('  [FAIL] ' + name + (extra ? '  ' + extra : ''))
  }
}

function section (title) {
  console.log('\n== ' + title + ' ==')
}

function bodyOf (msg) {
  const c = msg && msg.result && msg.result.content
  if (!Array.isArray(c) || !c.length) return null
  try {
    return JSON.parse(c[0].text)
  } catch {
    return c[0].text
  }
}

function errorOf (msg) {
  if (!msg) return 'no response'
  if (msg.error) return msg.error.message || JSON.stringify(msg.error)
  if (msg.result && msg.result.isError) {
    const c = msg.result.content
    return Array.isArray(c) && c[0] ? c[0].text : 'unknown tool error'
  }
  return null
}

async function main () {
  const child = spawn(exe, [], { stdio: ['pipe', 'pipe', 'pipe'] })

  let buffer = ''
  let nextId = 1
  let stderrTail = ''
  const pending = new Map()

  child.stderr.on('data', (d) => { stderrTail = (stderrTail + d.toString()).slice(-2000) })

  child.stdout.on('data', (chunk) => {
    buffer += chunk.toString()
    let idx
    while ((idx = buffer.indexOf('\n')) >= 0) {
      const line = buffer.slice(0, idx).trim()
      buffer = buffer.slice(idx + 1)
      if (!line) continue
      let msg
      try { msg = JSON.parse(line) } catch { continue }
      const w = pending.get(msg.id)
      if (w) { pending.delete(msg.id); clearTimeout(w.timer); w.resolve(msg) }
    }
  })

  function request (method, params) {
    const id = nextId++
    const payload = JSON.stringify({ jsonrpc: '2.0', id, method, params }) + '\n'
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        pending.delete(id)
        reject(new Error('超时 ' + method + '：' + stderrTail))
      }, CALL_TIMEOUT_MS)
      pending.set(id, { resolve, timer })
      child.stdin.write(payload)
    })
  }

  function notify (method, params) {
    child.stdin.write(JSON.stringify({ jsonrpc: '2.0', method, params }) + '\n')
  }

  const call = (name, args) => request('tools/call', { name, arguments: args || {} })

  async function expectOk (label, name, args) {
    const m = await call(name, args)
    const e = errorOf(m)
    ok(label, !e, e ? String(e).slice(0, 100) : '')
    return bodyOf(m)
  }

  async function expectErr (label, name, args) {
    const m = await call(name, args)
    const e = errorOf(m)
    ok(label, !!e, e ? String(e).slice(0, 90) : '(却返回了成功)')
    return e
  }

  const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

  // 把播放切到指定歌单的指定歌曲。ref 需要先在缓存里，所以先读一次该歌单回填。
  async function playFrom (listId, ref) {
    if (!listId || !ref) return false
    await call('lx_playlist_songs', { action: 'list', listId }).catch(() => {})
    const m = await call('lx_play', { listId, ref }).catch(() => null)
    return !!m && !errorOf(m)
  }

  let restore = null
  let testListId = null
  // 下面几个在 finally 里收尾时要用，所以声明在外层
  let queueListId = null
  let queueCurrent = null
  let fallbackListId = null

  try {
    // ================================================================ 协议
    section('协议层')

    const init = await request('initialize', {
      protocolVersion: '2025-06-18',
      capabilities: {},
      clientInfo: { name: 'full', version: '1.0' },
    })
    ok('initialize 成功', !!(init.result && init.result.serverInfo))
    ok('返回了会话级 instructions', !!(init.result && init.result.instructions),
      init.result && init.result.instructions ? init.result.instructions.length + ' 字符' : '')
    notify('notifications/initialized')

    const listMsg = await request('tools/list', {})
    const tools = (listMsg.result && listMsg.result.tools) || []
    const byName = Object.fromEntries(tools.map((t) => [t.name, t]))
    ok('tools/list 返回 9 个工具', tools.length === 9, String(tools.length))
    ok('每个工具都有 inputSchema 且为 object',
      tools.every((t) => t.inputSchema && t.inputSchema.type === 'object'))

    // 参数描述是否真的生成了（防止 tag 写错导致 schema 空壳）
    const ctrl = byName.lx_control
    const ctrlProps = ctrl && ctrl.inputSchema.properties
    ok('lx_control 的 action 有枚举说明',
      !!(ctrlProps && ctrlProps.action && ctrlProps.action.description))
    ok('lx_search 的 keyword 被标为必填',
      !!(byName.lx_search && byName.lx_search.inputSchema.required
        && byName.lx_search.inputSchema.required.includes('keyword')))

    // ============================================================ lx_status
    section('lx_status')

    const st0 = await expectOk('默认调用', 'lx_status', {})
    ok('包含 status 与 name 字段', !!(st0 && st0.status !== undefined && st0.name !== undefined),
      st0 ? JSON.stringify(st0).slice(0, 110) : '')
    // 默认返回就带 volume 与 mute，直接从 st0 取下要恢复的值即可。
    // （曾经的坑：默认集不含这两个字段，从 st0 上读永远是 undefined，
    //   「收尾恢复音量」那段等于没跑——真实踩到过：跑完测试音量永远停在 55。
    //   这里顺带守住「默认即含」这个行为，免得哪天又被改回去。）
    ok('默认返回即含 volume 与 mute',
      !!(st0 && st0.volume !== undefined && st0.mute !== undefined),
      st0 ? JSON.stringify(st0).slice(0, 110) : '')
    restore = {
      status: st0 && st0.status,
      volume: st0 && st0.volume,
      mute: st0 && st0.mute,
    }

    const stFiltered = await expectOk('filter 指定字段', 'lx_status', { filter: 'name,singer' })
    ok('只返回请求的字段', !!(stFiltered && stFiltered.name !== undefined && stFiltered.status === undefined),
      JSON.stringify(stFiltered))

    await expectOk('filter 传无效字段也不报错', 'lx_status', { filter: '不存在的字段' })

    // =========================================================== lx_control
    section('lx_control')

    // 先把"有歌在播"这个前提建立起来：否则 pause / seek / volume 都没有作用对象，
    // 测出来的失败会是假失败。顺便在这里把主测试歌单建好。
    const preList = bodyOf(await call('lx_playlists', { action: 'list' }))
    if (Array.isArray(preList)) {
      const stale = preList.filter((p) => p.name === TEST_LIST)
      if (stale.length) {
        await call('lx_playlists', { action: 'remove', ids: stale.map((p) => p.id) })
        console.log('  (清理了 ' + stale.length + ' 个残留测试歌单)')
      }
    }

    const warm = bodyOf(await call('lx_search', { keyword: '光良 童话', limit: 3 }))
    const warmRef = warm && warm.songs && warm.songs[0] && warm.songs[0].ref
    const prepList = bodyOf(await call('lx_playlists', { action: 'create', name: TEST_LIST }))
    testListId = prepList && prepList.id
    ok('准备：主测试歌单就绪', !!testListId, testListId || '')
    if (warmRef && testListId) {
      await call('lx_playlist_songs', { action: 'add', listId: testListId, refs: [warmRef] })
      await expectOk('准备：播放一首歌', 'lx_play', { listId: testListId, ref: warmRef })
      await sleep(6000)
      const warmed = bodyOf(await call('lx_status', { filter: 'status,name,duration' }))
      ok('准备：播放器可读状态', !!(warmed && warmed.status), warmed ? JSON.stringify(warmed) : '')
    }

    await expectOk('pause', 'lx_control', { action: 'pause' })
    // 和音量一样，播放状态要等 renderer 处理完再回传，400ms 处在不稳定边缘。
    await sleep(1500)
    const stPaused = await expectOk('pause 后读状态', 'lx_status', { filter: 'status' })
    ok('状态为 paused', !!(stPaused && stPaused.status === 'paused'), JSON.stringify(stPaused))

    await expectOk('play', 'lx_control', { action: 'play' })
    await sleep(1500)
    const stPlaying = await call('lx_status', { filter: 'status' })
    ok('play 后状态不是错误', !errorOf(stPlaying))

    // seek 的上限由服务端按当前歌曲时长校验；当前歌还没加载出时长时会被合法拒绝，
    // 所以先读 duration 再决定测不测，避免把 LX 的状态当成 server 的缺陷。
    const durBody = bodyOf(await call('lx_status', { filter: 'duration' }))
    const dur = durBody && durBody.duration
    if (typeof dur === 'number' && dur > 30) {
      await expectOk('seek 到 30 秒', 'lx_control', { action: 'seek', value: 30 })
    } else {
      ok('seek：当前歌曲 duration=' + dur + '，无法构造合法跳转（跳过，非失败）', true)
    }
    await expectErr('seek 缺 value 应报错', 'lx_control', { action: 'seek' })

    await expectOk('volume 设为 55', 'lx_control', { action: 'volume', value: 55 })
    // 音量是 renderer 侧生效后再回传状态，400ms 不够（实测约需 1~2s）。
    await sleep(2000)
    const stVol = await call('lx_status', { filter: 'volume' })
    const volBody = bodyOf(stVol)
    ok('音量已变为 55', !!(volBody && volBody.volume === 55), JSON.stringify(volBody))
    await expectErr('volume 超出范围应报错', 'lx_control', { action: 'volume', value: 101 })
    await expectErr('volume 为 0 应报错', 'lx_control', { action: 'volume', value: 0 })

    // 相对增减。基准是上一步刚设的 55；server 侧有短期音量记忆，
    // 所以连续调用不会因为 renderer 回传延迟而丢步。
    const downStep = await expectOk('volume_down 用默认步长', 'lx_control', { action: 'volume_down' })
    ok('volume_down 返回新音量 51', !!(downStep && downStep.volume === 51), JSON.stringify(downStep))
    await sleep(2000)
    const stDown = bodyOf(await call('lx_status', { filter: 'volume' }))
    ok('volume_down 后实际音量为 51', !!(stDown && stDown.volume === 51), JSON.stringify(stDown))

    const upStep = await expectOk('volume_up 指定步长 22', 'lx_control', { action: 'volume_up', value: 22 })
    ok('volume_up 返回新音量 73', !!(upStep && upStep.volume === 73), JSON.stringify(upStep))
    await sleep(2000)
    const stUp = bodyOf(await call('lx_status', { filter: 'volume' }))
    ok('volume_up 后实际音量为 73', !!(stUp && stUp.volume === 73), JSON.stringify(stUp))

    const topStep = await expectOk('volume_up 越过上限不报错', 'lx_control', { action: 'volume_up', value: 100 })
    ok('返回值被夹在 100', !!(topStep && topStep.volume === 100), JSON.stringify(topStep))
    await sleep(2000)
    const stTop = bodyOf(await call('lx_status', { filter: 'volume' }))
    ok('实际上限被夹在 100', !!(stTop && stTop.volume === 100), JSON.stringify(stTop))

    const bottomStep = await expectOk('volume_down 越过下限不报错', 'lx_control', { action: 'volume_down', value: 100 })
    ok('返回值被夹在 1', !!(bottomStep && bottomStep.volume === 1), JSON.stringify(bottomStep))
    await sleep(2000)
    const stBottom = bodyOf(await call('lx_status', { filter: 'volume' }))
    ok('实际下限被夹在 1', !!(stBottom && stBottom.volume === 1), JSON.stringify(stBottom))

    await expectErr('volume_up 步长越界应报错', 'lx_control', { action: 'volume_up', value: 101 })
    await expectErr('volume_down 负步长应报错', 'lx_control', { action: 'volume_down', value: -1 })

    // 复位回 55：后面的 mute 测试与收尾恢复都以此为基准。
    await expectOk('音量复位为 55', 'lx_control', { action: 'volume', value: 55 })
    await sleep(2000)

    await expectOk('mute 开', 'lx_control', { action: 'mute', mute: true })
    await sleep(400)
    const stMuteOn = bodyOf(await call('lx_status', { filter: 'mute' }))
    ok('静音已生效', !!(stMuteOn && stMuteOn.mute === true), JSON.stringify(stMuteOn))
    await expectOk('mute 关', 'lx_control', { action: 'mute', mute: false })
    await sleep(400)
    const stMuteOff = bodyOf(await call('lx_status', { filter: 'mute' }))
    ok('取消静音已生效', !!(stMuteOff && stMuteOff.mute === false), JSON.stringify(stMuteOff))

    await expectOk('collect', 'lx_control', { action: 'collect' })
    await expectOk('uncollect', 'lx_control', { action: 'uncollect' })
    await expectErr('未知 action 应报错', 'lx_control', { action: '不存在的动作' })

    // ============================================================= lx_lyric
    section('lx_lyric')

    const lyric = await expectOk('默认取当前歌词', 'lx_lyric', {})
    ok('返回的是字符串歌词', typeof lyric === 'string', typeof lyric)
    const lyricAll = await expectOk('type=all', 'lx_lyric', { type: 'all' })
    ok('返回四个歌词字段',
      !!(lyricAll && 'lyric' in lyricAll && 'tlyric' in lyricAll && 'rlyric' in lyricAll && 'lxlyric' in lyricAll),
      lyricAll ? Object.keys(lyricAll).join(',') : '')

    // ============================================================= lx_queue
    section('lx_queue')

    const queue = await expectOk('获取队列', 'lx_queue', {})
    ok('含 listId/currentIndex/count/songs',
      !!(queue && 'listId' in queue && 'currentIndex' in queue && queue.count !== undefined && Array.isArray(queue.songs)),
      queue ? 'count=' + queue.count + ' index=' + queue.currentIndex : '')

    queueCurrent = queue && Array.isArray(queue.songs) ? queue.songs[queue.currentIndex] : null
    queueListId = queue && queue.listId
    if (queue && queue.count > 0) {
      ok('队列里的歌曲都带 ref', queue.songs.every((x) => x.ref), queueCurrent ? queueCurrent.ref : '')
      ok('队列结果不含完整 meta', queue.songs.every((x) => x.meta === undefined))
    } else {
      ok('队列为空（当前无播放列表），跳过 ref 检查', true, 'count=0')
    }

    // ============================================================ lx_search
    section('lx_search')

    const s1 = await expectOk('基本搜索', 'lx_search', { keyword: '夜曲', limit: 5 })
    ok('返回 songs', !!(s1 && Array.isArray(s1.songs)), s1 ? s1.count + ' 条' : '')
    ok('每条都有 ref 和 name',
      !!(s1 && s1.songs.length && s1.songs.every((x) => x.ref && x.name !== undefined)))

    for (const src of ['kw', 'kg', 'mg', 'tx', 'wy']) {
      const r = await call('lx_search', { keyword: '童话', source: src, limit: 3 })
      const e = errorOf(r)
      const b = bodyOf(r)
      ok('单音源 ' + src, !e, e ? String(e).slice(0, 70) : (b ? b.count + ' 条' : ''))
    }

    const sOrder = await expectOk('order 指定音源顺序', 'lx_search', { keyword: '童话', order: 'mg,kw', limit: 5 })
    ok('order=mg,kw 只出这两个源', !!(sOrder && sOrder.songs.every((x) => x.source === 'mg' || x.source === 'kw')),
      sOrder ? [...new Set(sOrder.songs.map((x) => x.source))].join(',') : '')

    const sDedup = await expectOk('dedup 去重', 'lx_search', { keyword: '童话', dedup: true, limit: 10 })
    const dedupKeys = new Set((sDedup ? sDedup.songs : []).map((x) => x.name + '|' + x.singer))
    ok('去重后无重复的 名|歌手 组合',
      !!(sDedup && dedupKeys.size === sDedup.songs.length),
      sDedup ? sDedup.count + ' 条 / ' + dedupKeys.size + ' 唯一' : '')

    const sSinger = await expectOk('matchSinger 歌手匹配', 'lx_search', { keyword: '童话', matchSinger: '光良', limit: 8 })
    ok('带回了 matchLevel 标注',
      !!(sSinger && sSinger.songs.length && sSinger.songs[0].matchLevel),
      sSinger && sSinger.songs[0] ? sSinger.songs[0].matchLevel : '')

    const sQuality = await expectOk('minQuality=flac', 'lx_search', { keyword: '童话', minQuality: 'flac', limit: 10 })
    ok('过滤后结果都满足 flac 及以上',
      !!(sQuality && sQuality.songs.every((x) => !x.quality || x.quality.some((q) => q === 'flac' || q === 'flac24bit'))),
      sQuality ? sQuality.count + ' 条' : '')

    const sPage = await expectOk('第 2 页', 'lx_search', { keyword: '童话', page: 2, limit: 5 })
    ok('返回 page=2', !!(sPage && sPage.page === 2), sPage ? 'page=' + sPage.page : '')

    await expectErr('空关键词应报错', 'lx_search', { keyword: '   ' })

    // 多音源模糊搜索对几乎任何关键词都会返回结果，"0 结果"很难构造出来，
    // 所以这里只验证冷僻词不报错、返回结构完整。
    const sNone = await call('lx_search', { keyword: 'zzzqqq不存在的歌名xxyy', limit: 5 })
    const noneErr = errorOf(sNone)
    const noneBody = bodyOf(sNone)
    ok('冷僻关键词不报错且结构完整',
      !noneErr && !!(noneBody && Array.isArray(noneBody.songs) && typeof noneBody.count === 'number'),
      noneErr ? String(noneErr).slice(0, 60) : 'count=' + (noneBody && noneBody.count))

    // ========================================================= lx_playlists
    section('lx_playlists')

    const before = await expectOk('list', 'lx_playlists', { action: 'list' })
    ok('返回歌单数组', Array.isArray(before), Array.isArray(before) ? before.length + ' 个' : typeof before)

    // 记住一个"安全落点"歌单：收尾时先把播放挪过去，再删测试歌单。
    if (Array.isArray(before)) {
      const safe = before.find((p) => p.name !== TEST_LIST && p.id)
      fallbackListId = safe ? safe.id : null
    }

    // 新建 → 删除 走一轮（用临时名，不动主测试歌单）
    const tmpName = TEST_LIST + '-TMP'
    const staleTmp = Array.isArray(before) ? before.filter((p) => p.name === tmpName) : []
    if (staleTmp.length) await call('lx_playlists', { action: 'remove', ids: staleTmp.map((p) => p.id) })

    const created = await expectOk('create 临时歌单', 'lx_playlists', { action: 'create', name: tmpName })
    ok('返回新建歌单的 id', !!(created && created.id), created ? created.id : '')
    if (created && created.id) {
      await expectOk('remove 刚建的歌单', 'lx_playlists', { action: 'remove', ids: [created.id] })
      const afterRm = bodyOf(await call('lx_playlists', { action: 'list' }))
      ok('歌单已不在列表里', !!(Array.isArray(afterRm) && !afterRm.some((p) => p.id === created.id)))
    }

    await expectErr('create 缺 name 应报错', 'lx_playlists', { action: 'create' })
    await expectErr('remove 缺 ids 应报错', 'lx_playlists', { action: 'remove' })
    await expectErr('未知 action 应报错', 'lx_playlists', { action: '不存在的动作' })

    // =================================================== lx_playlist_songs
    section('lx_playlist_songs')

    const ghost = await call('lx_playlist_songs', { action: 'list', listId: 'userlist_不存在的id' })
    const ghostErr = errorOf(ghost)
    const ghostBody = bodyOf(ghost)
    ok('list 不存在的歌单：报错或返回空列表都算合理',
      !!ghostErr || !!(ghostBody && ghostBody.count === 0),
      ghostErr ? '报错 ' + String(ghostErr).slice(0, 50) : JSON.stringify(ghostBody))

    await expectErr('缺 listId 应报错', 'lx_playlist_songs', { action: 'list' })

    const refs = (s1 && s1.songs ? s1.songs.slice(0, 3).map((x) => x.ref) : [])
    ok('拿到了 3 个待添加的 ref', refs.length === 3, refs.join(','))

    // 歌单里已经有 warmup 放进去的那首，所以按增量断言，别写死数量。
    const beforeAdd = await expectOk('add 前回读', 'lx_playlist_songs', { action: 'list', listId: testListId })
    const n0 = beforeAdd ? beforeAdd.count : 0

    await expectOk('add 3 首', 'lx_playlist_songs', { action: 'add', listId: testListId, refs })

    const inList = await expectOk('list 回读', 'lx_playlist_songs', { action: 'list', listId: testListId })
    ok('歌单里多了 3 首', !!(inList && inList.count === n0 + 3), inList ? n0 + ' → ' + inList.count : '')
    ok('回读的歌曲都带 ref', !!(inList && inList.songs.every((x) => x.ref)))

    // remove：移除第一首
    const removeId = inList && inList.songs[0] && inList.songs[0].ref
    await expectOk('remove 1 首', 'lx_playlist_songs', { action: 'remove', listId: testListId, songIds: [removeId] })
    const afterRemove = await expectOk('remove 后回读', 'lx_playlist_songs', { action: 'list', listId: testListId })
    ok('少了 1 首', !!(afterRemove && afterRemove.count === n0 + 2), afterRemove ? afterRemove.count + ' 首' : '')

    // overwrite：用 1 首替换整表
    const owOk = await call('lx_playlist_songs', { action: 'overwrite', listId: testListId, refs: [refs[0]] })
    if (!errorOf(owOk)) {
      const afterOw = await expectOk('overwrite 后回读', 'lx_playlist_songs', { action: 'list', listId: testListId })
      ok('整表被替换为 1 首', !!(afterOw && afterOw.count === 1), afterOw ? afterOw.count + ' 首' : '')
    } else {
      ok('overwrite', false, String(errorOf(owOk)).slice(0, 90))
    }

    await expectErr('add 用无效 ref 应报错', 'lx_playlist_songs',
      { action: 'add', listId: testListId, refs: ['无效_ref_zzz'] })

    // ============================================================== lx_play
    section('lx_play')

    if (testListId && refs.length) {
      await expectOk('播放测试歌单里的歌', 'lx_play', { listId: testListId, ref: refs[0] })
      await sleep(4000)
      const afterPlay = bodyOf(await call('lx_status', { filter: 'status,name' }))
      ok('播放后状态可读', !!(afterPlay && afterPlay.status), afterPlay ? JSON.stringify(afterPlay) : '')

      // 此刻队列必然非空，正好验证 lx_queue 真的给出了可用的 ref
      const qAfter = bodyOf(await call('lx_queue'))
      ok('播放后队列非空', !!(qAfter && qAfter.count > 0), qAfter ? 'count=' + qAfter.count : '')
      ok('队列里的歌都带 ref（可用于 lx_play）',
        !!(qAfter && qAfter.songs.length > 0 && qAfter.songs.every((x) => x.ref)),
        qAfter ? 'index=' + qAfter.currentIndex : '')
      ok('队列结果不含完整 meta（避免撑爆上下文）',
        !!(qAfter && qAfter.songs.every((x) => x.meta === undefined)))

      // 用队列里给出的 ref 反手播一次，验证 ref 真的可用
      const qRef = qAfter && qAfter.songs[0] && qAfter.songs[0].ref
      if (qRef) {
        await expectOk('用队列返回的 ref 播放', 'lx_play', { listId: testListId, ref: qRef })
      }
    }

    await expectErr('无效 ref 应报错', 'lx_play', { listId: testListId || 'userlist_x', ref: '无效_ref_zzz' })

    // ========================================================= lx_batch_add
    section('lx_batch_add')

    const b1 = await expectOk('批量添加 3 首', 'lx_batch_add', {
      playlistName: TEST_LIST,
      songs: ['周杰伦 - 晴天', '光良 - 童话', '陈奕迅 十年'],
    })
    ok('返回 summary 五元组',
      !!(b1 && b1.summary && 'total' in b1.summary && 'found' in b1.summary
        && 'added' in b1.summary && 'failed' in b1.summary && 'uncertain' in b1.summary),
      b1 ? JSON.stringify(b1.summary) : '')
    ok('三首都成功', !!(b1 && b1.summary.added === 3))
    ok('复用已有歌单（created=false）', !!(b1 && b1.playlist.created === false))
    ok('added 列表带 ref', !!(b1 && b1.added.length === 3 && b1.added.every((x) => x.ref)))

    const b2 = await expectOk('混入一首冷僻歌曲', 'lx_batch_add', {
      playlistName: TEST_LIST,
      songs: ['zzzqqq不存在的歌xxyy', '光良 - 童话'],
    })
    ok('汇总数字自洽（total = added + failed）',
      !!(b2 && b2.summary.total === b2.summary.added + b2.summary.failed),
      b2 ? JSON.stringify(b2.summary) : '')
    ok('failed 列表长度与计数一致', !!(b2 && b2.failed.length === b2.summary.failed))
    ok('明确要求的那首确实入库',
      !!(b2 && b2.added.some((x) => x.name && x.name.includes('童话'))))

    const b3 = await expectOk('includeAll 不过滤', 'lx_batch_add', {
      playlistName: TEST_LIST,
      songs: ['童话'],
      includeAll: true,
    })
    ok('includeAll 可正常执行', !!(b3 && b3.summary.added >= 1), b3 ? JSON.stringify(b3.summary) : '')

    await expectErr('缺 playlistName 应报错', 'lx_batch_add', { songs: ['童话'] })
    await expectErr('缺 songs 应报错', 'lx_batch_add', { playlistName: TEST_LIST })

    // ============================================================== 可靠性
    section('可靠性')

    // 连续调用
    let seqOk = 0
    for (let i = 0; i < 20; i++) {
      const m = await call('lx_status', { filter: 'status' })
      if (!errorOf(m)) seqOk++
    }
    ok('连续 20 次调用全部成功', seqOk === 20, seqOk + '/20')

    // 并发（超过服务端 5 并发的限流阈值，验证限流生效且不报错）
    const conc = await Promise.all(
      Array.from({ length: 8 }, () => call('lx_status', { filter: 'name' })),
    )
    const concErr = conc.filter((m) => errorOf(m))
    ok('并发 8 个请求全部成功', concErr.length === 0,
      concErr.length ? '失败 ' + concErr.length + '：' + String(errorOf(concErr[0])).slice(0, 70) : '8/8')

    // 并发搜索（更容易触发 ECONNRESET 的场景）
    const searches = await Promise.all(
      ['童话', '晴天', '十年', '夜曲', '黄昏', '简单爱'].map((kw) =>
        call('lx_search', { keyword: kw, limit: 3 })),
    )
    const searchErr = searches.filter((m) => errorOf(m))
    ok('并发 6 个搜索全部成功', searchErr.length === 0,
      searchErr.length ? '失败 ' + searchErr.length + '：' + String(errorOf(searchErr[0])).slice(0, 70) : '6/6')

    // 同一个 ref 重复使用
    if (refs.length) {
      const repeat = await Promise.all(
        Array.from({ length: 5 }, () => call('lx_playlist_songs', { action: 'list', listId: testListId })),
      )
      ok('重复读取同一歌单 5 次全部成功', repeat.every((m) => !errorOf(m)))
    }
  } finally {
    // ================================================================ 恢复
    section('清理与恢复')

    // 顺序至关重要：必须先把播放挪出测试歌单，**再**删除它。
    // 否则播放队列会指向一个已不存在的歌单，LX 播放器会卡死——
    // 表现为 status 恒为 playing，但 duration/progress 恒为 0，play/pause 都失效。
    // （这个坑真实踩到过：整个播放器需要重启才能恢复。）
    let movedAway = false
    if (queueListId && queueListId !== testListId && queueCurrent && queueCurrent.ref) {
      movedAway = await playFrom(queueListId, queueCurrent.ref)
    }
    if (!movedAway && fallbackListId) {
      const fb = bodyOf(await call('lx_playlist_songs', { action: 'list', listId: fallbackListId }).catch(() => null))
      const fbRef = fb && fb.songs && fb.songs[0] && fb.songs[0].ref
      movedAway = await playFrom(fallbackListId, fbRef)
    }
    if (movedAway) await sleep(2500)

    if (testListId) {
      const rm = await call('lx_playlists', { action: 'remove', ids: [testListId] }).catch(() => null)
      ok('测试歌单已删除', !rm || !errorOf(rm), rm ? String(errorOf(rm) || '').slice(0, 60) : '')
    }

    if (restore) {
      if (typeof restore.volume === 'number' && restore.volume !== 55) {
        await call('lx_control', { action: 'volume', value: restore.volume }).catch(() => {})
      }
      if (typeof restore.mute === 'boolean') {
        await call('lx_control', { action: 'mute', mute: restore.mute }).catch(() => {})
      }
      const stFinal = bodyOf(await call('lx_status', { filter: 'name,singer,status' }).catch(() => null))
      console.log('  收尾后播放状态：' + JSON.stringify(stFinal) + (movedAway ? '（已把播放挪出测试歌单）' : ''))
    }

    console.log('\n结果：' + pass + ' 通过 / ' + fail + ' 失败')
    if (failures.length) console.log('失败项：\n  - ' + failures.join('\n  - '))

    child.stdin.end()
    child.kill()
  }

  process.exit(fail === 0 ? 0 : 1)
}

main().catch((e) => {
  console.error('\n测试异常终止：' + (e && e.message))
  process.exit(1)
})
