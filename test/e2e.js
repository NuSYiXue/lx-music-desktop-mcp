#!/usr/bin/env node
'use strict'

// 对 lx-music-mcp.exe 做一次端到端冒烟测试。
//
// 手写 JSON-RPC over stdio，不依赖任何 npm 包——这样测试脚本本身
// 不会给"零依赖"这个目标开口子。
//
// 用法：
//   node test/e2e.js [exe路径]
//
// 前提：LX Music 正在运行，且已开启 Open API 服务。
// 副作用：会创建并删除一个名为 __MCP-SMOKE-TEST__ 的歌单；
//         lx_play 一节会切换当前播放，请先告知用户。
// 退出码：0 = 全部通过。

const { spawn } = require('node:child_process')
const path = require('node:path')
const fs = require('node:fs')

const exe = process.argv[2] || path.join(__dirname, '..', 'mcp', 'lx-music-mcp.exe')

if (!fs.existsSync(exe)) {
  console.error('找不到可执行文件：' + exe)
  console.error('请先构建：go build -trimpath -o mcp/lx-music-mcp.exe ./cmd/lx-music-mcp')
  process.exit(1)
}

const TEST_PLAYLIST = '__MCP-SMOKE-TEST__'
const CALL_TIMEOUT_MS = 120000

let pass = 0
let fail = 0

function ok (name, cond, extra) {
  if (cond) {
    pass++
    console.log('  [ok]   ' + name + (extra ? '  ' + extra : ''))
  } else {
    fail++
    console.log('  [FAIL] ' + name + (extra ? '  ' + extra : ''))
  }
}

function section (title) {
  console.log('\n' + title)
}

function parseToolText (msg) {
  const content = msg && msg.result && msg.result.content
  if (!Array.isArray(content) || !content.length) return null
  const text = content[0].text
  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}

function toolErrorOf (msg) {
  if (!msg) return 'no response'
  if (msg.error) return msg.error.message || JSON.stringify(msg.error)
  if (msg.result && msg.result.isError) {
    const c = msg.result.content
    return Array.isArray(c) && c[0] ? c[0].text : 'unknown tool error'
  }
  return null
}

async function main () {
  // stderr 必须自己接管：用 'inherit' 的话 child.stderr 是 null。
  const child = spawn(exe, [], { stdio: ['pipe', 'pipe', 'pipe'] })

  let buffer = ''
  const pending = new Map()
  let nextId = 1
  let stderrTail = ''

  child.stderr.on('data', (d) => {
    stderrTail = (stderrTail + d.toString()).slice(-2000)
  })

  child.stdout.on('data', (chunk) => {
    buffer += chunk.toString()
    let idx
    while ((idx = buffer.indexOf('\n')) >= 0) {
      const line = buffer.slice(0, idx).trim()
      buffer = buffer.slice(idx + 1)
      if (!line) continue
      let msg
      try {
        msg = JSON.parse(line)
      } catch {
        continue
      }
      const waiter = pending.get(msg.id)
      if (waiter) {
        pending.delete(msg.id)
        clearTimeout(waiter.timer)
        waiter.resolve(msg)
      }
    }
  })

  function request (method, params) {
    const id = nextId++
    const payload = JSON.stringify({ jsonrpc: '2.0', id, method, params }) + '\n'
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        pending.delete(id)
        reject(new Error('超时（' + method + '）：' + stderrTail))
      }, CALL_TIMEOUT_MS)
      pending.set(id, { resolve, timer })
      child.stdin.write(payload)
    })
  }

  function notify (method, params) {
    child.stdin.write(JSON.stringify({ jsonrpc: '2.0', method, params }) + '\n')
  }

  function callTool (name, args) {
    return request('tools/call', { name, arguments: args })
  }

  try {
    // ---------------------------------------------------------------- 握手
    section('握手与工具清单')

    const init = await request('initialize', {
      protocolVersion: '2025-06-18',
      capabilities: {},
      clientInfo: { name: 'e2e', version: '1.0' },
    })
    ok('initialize 成功', !!(init.result && init.result.serverInfo), init.result && init.result.serverInfo && init.result.serverInfo.name)
    notify('notifications/initialized')

    const list = await request('tools/list', {})
    const tools = (list.result && list.result.tools) || []
    const names = tools.map((t) => t.name).sort()
    const expected = [
      'lx_batch_add', 'lx_control', 'lx_lyric', 'lx_play', 'lx_playlist_songs',
      'lx_playlists', 'lx_queue', 'lx_search', 'lx_status',
    ]
    ok('tools/list 返回 9 个工具', names.length === 9, names.join(','))
    ok('工具名与预期一致', JSON.stringify(names) === JSON.stringify(expected))
    ok('每个工具都有描述', tools.every((t) => t.description && t.description.length > 10))
    ok('每个工具都有 inputSchema', tools.every((t) => t.inputSchema && t.inputSchema.type === 'object'))

    // ------------------------------------------------------------ 只读检查
    section('只读：状态 / 歌单 / 队列')

    const status = await callTool('lx_status', {})
    const statusErr = toolErrorOf(status)
    ok('lx_status 无错误', !statusErr, statusErr || '')
    const statusBody = parseToolText(status)
    ok('lx_status 返回了歌曲字段', !!(statusBody && (statusBody.name !== undefined || statusBody.status !== undefined)),
      statusBody ? JSON.stringify(statusBody).slice(0, 120) : '')

    const playlists = await callTool('lx_playlists', { action: 'list' })
    ok('lx_playlists 无错误', !toolErrorOf(playlists), toolErrorOf(playlists) || '')
    const plBody = parseToolText(playlists)
    ok('lx_playlists 返回数组', Array.isArray(plBody), Array.isArray(plBody) ? plBody.length + ' 个歌单' : typeof plBody)

    const queue = await callTool('lx_queue', {})
    ok('lx_queue 无错误', !toolErrorOf(queue), toolErrorOf(queue) || '')
    const queueBody = parseToolText(queue)
    ok('lx_queue 返回 count 字段', !!(queueBody && queueBody.count !== undefined))

    // ---------------------------------------------------------------- 搜索
    section('搜索')

    const search = await callTool('lx_search', { keyword: '夜曲', limit: 5, dedup: true })
    ok('lx_search 无错误', !toolErrorOf(search), toolErrorOf(search) || '')
    const searchBody = parseToolText(search)
    ok('lx_search 返回 songs 数组', !!(searchBody && Array.isArray(searchBody.songs)), searchBody ? searchBody.count + ' 条' : '')
    const songs = (searchBody && searchBody.songs) || []
    ok('每条结果都有 ref', songs.length > 0 && songs.every((s) => s.ref && s.name !== undefined))
    ok('结果不含完整 meta（上下文省着用）', songs.every((s) => s.meta === undefined))

    const firstRef = songs.length ? songs[0].ref : null

    const searchQuality = await callTool('lx_search', { keyword: '夜曲', limit: 5, minQuality: 'flac' })
    const qualityBody = parseToolText(searchQuality)
    ok('minQuality=flac 可用', !toolErrorOf(searchQuality), qualityBody ? qualityBody.count + ' 条' : '')

    // ------------------------------------------------------- 歌单写入与清理
    section('批量入库（测试歌单 ' + TEST_PLAYLIST + '）')

    const batch = await callTool('lx_batch_add', {
      playlistName: TEST_PLAYLIST,
      songs: ['周杰伦 - 夜曲', '光良 - 童话'],
      maxPerQuery: 10,
    })
    const batchErr = toolErrorOf(batch)
    ok('lx_batch_add 无错误', !batchErr, batchErr || '')
    const batchBody = parseToolText(batch)
    ok('返回 summary', !!(batchBody && batchBody.summary))
    ok('两首都加进去了', !!(batchBody && batchBody.summary && batchBody.summary.added === 2),
      batchBody && batchBody.summary ? JSON.stringify(batchBody.summary) : '')

    const testListId = batchBody && batchBody.playlist && batchBody.playlist.id

    if (testListId) {
      const songsInList = await callTool('lx_playlist_songs', { action: 'list', listId: testListId })
      const inListBody = parseToolText(songsInList)
      ok('歌单里确实有 2 首', !!(inListBody && inListBody.count === 2), inListBody ? inListBody.count + ' 首' : '')
      ok('读歌单也回填了 ref', !!(inListBody && inListBody.songs && inListBody.songs.every((s) => s.ref)))

      // ------------------------------------------------------------ 播放
      section('播放（会切换当前播放）')
      const playRef = (inListBody && inListBody.songs && inListBody.songs[0] && inListBody.songs[0].ref) || firstRef
      if (playRef) {
        const play = await callTool('lx_play', { listId: testListId, ref: playRef })
        ok('lx_play 无错误', !toolErrorOf(play), toolErrorOf(play) || '')

        // 播放是异步的，给几秒钟再确认
        await new Promise((r) => setTimeout(r, 4000))
        const after = await callTool('lx_status', { filter: 'status,name,singer' })
        const afterBody = parseToolText(after)
        ok('播放后状态可读', !!(afterBody && afterBody.status), afterBody ? JSON.stringify(afterBody) : '')
      } else {
        ok('有可播放的 ref', false, '没拿到任何 ref')
      }

      // ------------------------------------------------------------ 清理
      section('清理测试歌单')
      const removed = await callTool('lx_playlists', { action: 'remove', ids: [testListId] })
      ok('测试歌单已删除', !toolErrorOf(removed), toolErrorOf(removed) || '')

      const after2 = await callTool('lx_playlists', { action: 'list' })
      const after2Body = parseToolText(after2)
      ok('确认测试歌单已不在列表里',
        Array.isArray(after2Body) && !after2Body.some((p) => p.id === testListId))
    }

    // ---------------------------------------------------------------- 收尾
    console.log('\n结果：' + pass + ' 通过 / ' + fail + ' 失败')
  } finally {
    child.stdin.end()
    child.kill()
  }

  process.exit(fail === 0 ? 0 : 1)
}

main().catch((e) => {
  console.error('\n测试异常终止：' + (e && e.message))
  process.exit(1)
})
