<script setup>
import { computed, ref } from "vue";

const tools = [
  { name: "gm.query", desc: "标准问答，返回最终答案。" },
  { name: "gm.get_session", desc: "根据 session_id 拉取会话历史。" },
  { name: "gm.stream_query", desc: "流式问答，适合长文本场景。" }
];

const authMode = ref("login");
const username = ref("");
const password = ref("");
const token = ref(localStorage.getItem("gm_access_token") || "");
const refreshToken = ref(localStorage.getItem("gm_refresh_token") || "");

const sessionId = ref("");
const sessions = ref([]);
const modelType = ref("auto");
const useRAG = ref(true);
const inputText = ref("");
const messages = ref([]);
const citations = ref([]);
const attachments = ref([]);
const statusText = ref("");
const busy = ref(false);
const selectedFile = ref(null);
const errorText = ref("");

const isAuthed = computed(() => token.value !== "");
const filteredTools = computed(() => {
  const q = inputText.value.trim();
  if (!q.startsWith("/")) return tools;
  return tools.filter((tool) => tool.name.includes(q.slice(1)));
});

function setAuth(data) {
  token.value = data.access_token || "";
  refreshToken.value = data.refresh_token || "";
  localStorage.setItem("gm_access_token", token.value);
  localStorage.setItem("gm_refresh_token", refreshToken.value);
}

async function doAuth(mode) {
  errorText.value = "";
  if (!username.value.trim() || !password.value) {
    errorText.value = "用户名和密码不能为空";
    return;
  }
  busy.value = true;
  try {
    const url = mode === "register" ? "/auth/register" : "/auth/login";
    const payload =
      mode === "register"
        ? { username: username.value.trim(), password: password.value }
        : { username: username.value.trim(), password: password.value, device_id: "web-console" };
    const res = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });
    const body = await res.json();
    if (!res.ok || body.code !== 0) {
      errorText.value = body.message || "请求失败";
      return;
    }
    if (mode === "register") {
      authMode.value = "login";
      statusText.value = "注册成功，请登录。";
      return;
    }
    setAuth(body.data || {});
    await refreshSessionList();
    statusText.value = "登录成功";
  } finally {
    busy.value = false;
  }
}

function logout() {
  token.value = "";
  refreshToken.value = "";
  sessionId.value = "";
  sessions.value = [];
  messages.value = [];
  citations.value = [];
  attachments.value = [];
  localStorage.removeItem("gm_access_token");
  localStorage.removeItem("gm_refresh_token");
}

async function refreshSessionList() {
  if (!isAuthed.value) return;
  const res = await fetch("/sessions?limit=50", {
    headers: { Authorization: `Bearer ${token.value}` }
  });
  const body = await res.json();
  if (res.ok && body.code === 0) {
    sessions.value = body.data?.items || [];
  }
}

async function sendQuestion() {
  const q = inputText.value.trim();
  if (!q) return;
  busy.value = true;
  errorText.value = "";
  statusText.value = "模型处理中...";
  citations.value = [];
  messages.value.push({ role: "user", content: q, ts: new Date().toISOString() });
  inputText.value = "";
  try {
    const res = await fetch("/query", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token.value}`
      },
      body: JSON.stringify({
        session_id: sessionId.value.trim(),
        question: q,
        model_type: modelType.value.trim() || "auto",
        use_rag: useRAG.value
      })
    });
    const body = await res.json();
    if (!res.ok || body.code !== 0) {
      errorText.value = body.message || "提问失败";
      messages.value.push({ role: "assistant", content: "请求失败，请重试。", ts: new Date().toISOString() });
      return;
    }
    const data = body.data || {};
    sessionId.value = data.session_id || sessionId.value;
    citations.value = data.citations || [];
    messages.value.push({ role: "assistant", content: data.answer || "", ts: new Date().toISOString() });
    statusText.value = `完成，request_id=${data.request_id || "-"}`;
    await refreshSessionList();
  } finally {
    busy.value = false;
  }
}

async function loadSessionByID(id) {
  const target = (id || sessionId.value || "").trim();
  if (!target) {
    errorText.value = "请先输入 session_id";
    return;
  }
  errorText.value = "";
  busy.value = true;
  try {
    const res = await fetch(`/session/${encodeURIComponent(target)}`, {
      headers: { Authorization: `Bearer ${token.value}` }
    });
    const body = await res.json();
    if (!res.ok || body.code !== 0) {
      errorText.value = body.message || "读取会话失败";
      return;
    }
    sessionId.value = body.data?.session_id || target;
    const msgs = body.data?.messages || [];
    messages.value = msgs.map((m) => ({ role: m.role, content: m.content, ts: m.created_at || "" }));
    statusText.value = `已加载会话：${body.data?.title || "-"}`;
  } finally {
    busy.value = false;
  }
}

function onFileChange(e) {
  selectedFile.value = e.target.files?.[0] || null;
}

function attachToQuestion(file) {
  const marker = `\n[附件] name=${file.original_name}; key=${file.file_key}; url=${file.download_url}`;
  inputText.value = (inputText.value + marker).trimStart();
}

async function uploadFile() {
  errorText.value = "";
  if (!selectedFile.value) {
    errorText.value = "请先选择文件";
    return;
  }
  busy.value = true;
  try {
    const fd = new FormData();
    fd.append("file", selectedFile.value);
    const res = await fetch("/attachments", {
      method: "POST",
      headers: { Authorization: `Bearer ${token.value}` },
      body: fd
    });
    const body = await res.json();
    if (!res.ok || body.code !== 0) {
      errorText.value = body.message || "上传失败";
      return;
    }
    const item = body.data;
    attachments.value.unshift(item);
    attachToQuestion(item);
    statusText.value = `上传成功：${item?.original_name || "-"}`;
    selectedFile.value = null;
    const el = document.getElementById("file-input");
    if (el) el.value = "";
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="shell">
    <header class="hero">
      <div>
        <h1>GopherMind 医疗问答控制台</h1>
        <p>登录、注册、聊天、附件上传、会话列表全部对齐后端接口。</p>
      </div>
      <div class="status">{{ statusText }}</div>
    </header>

    <main class="grid">
      <aside class="card tools">
        <h2>可用工具</h2>
        <p class="muted">输入以 <code>/</code> 开头可过滤工具。</p>
        <div class="tool" v-for="tool in filteredTools" :key="tool.name">
          <div class="tool-name">{{ tool.name }}</div>
          <div class="tool-desc">{{ tool.desc }}</div>
        </div>
      </aside>

      <section class="card auth" v-if="!isAuthed">
        <h2>认证</h2>
        <div class="tabs">
          <button :class="{ on: authMode === 'login' }" @click="authMode = 'login'">登录</button>
          <button :class="{ on: authMode === 'register' }" @click="authMode = 'register'">注册</button>
        </div>
        <input v-model="username" placeholder="用户名" />
        <input v-model="password" type="password" placeholder="密码" />
        <button :disabled="busy" @click="doAuth(authMode)">
          {{ busy ? "处理中..." : authMode === "login" ? "登录" : "注册" }}
        </button>
      </section>

      <section class="card chat" v-else>
        <div class="chat-top">
          <h2>聊天</h2>
          <div class="actions">
            <button class="ghost" :disabled="busy" @click="refreshSessionList">刷新会话列表</button>
            <button class="ghost" @click="logout">退出</button>
          </div>
        </div>

        <div class="row">
          <input v-model="sessionId" placeholder="session_id（可空，首次会自动创建）" />
          <input v-model="modelType" placeholder="model_type，默认 auto" />
          <label class="switch"><input type="checkbox" v-model="useRAG" /> Use RAG</label>
        </div>

        <div class="workspace">
          <div class="session-list">
            <div class="list-title">会话列表</div>
            <div
              class="session-item"
              v-for="item in sessions"
              :key="item.session_id"
              :class="{ active: item.session_id === sessionId }"
              @click="loadSessionByID(item.session_id)"
            >
              <div class="session-name">{{ item.title || item.session_id }}</div>
              <div class="session-meta">{{ item.last_message_at }}</div>
            </div>
          </div>

          <div class="chat-pane">
            <div class="messages">
              <div class="msg" v-for="(m, idx) in messages" :key="idx" :class="m.role">
                <div class="meta">{{ m.role }} {{ m.ts ? "· " + m.ts : "" }}</div>
                <div class="content">{{ m.content }}</div>
              </div>
            </div>

            <div class="composer">
              <div class="uploader">
                <input id="file-input" type="file" @change="onFileChange" />
                <button class="ghost" :disabled="busy" @click="uploadFile">上传并附加到问题</button>
              </div>
              <textarea v-model="inputText" rows="4" placeholder="输入医疗问题，支持 /gm.query 形式提示"></textarea>
              <button :disabled="busy || !inputText.trim()" @click="sendQuestion">
                {{ busy ? "处理中..." : "发送" }}
              </button>
            </div>
          </div>
        </div>

        <div class="citations" v-if="citations.length">
          <h3>引用证据</h3>
          <ul>
            <li v-for="ct in citations" :key="`${ct.doc_id}-${ct.chunk_id}`">
              {{ ct.doc_id }} / {{ ct.chunk_id }} / score={{ ct.score }}
            </li>
          </ul>
        </div>

        <div class="upload-list" v-if="attachments.length">
          <h3>已上传附件</h3>
          <ul>
            <li v-for="item in attachments" :key="item.attachment_id">
              <span>{{ item.original_name }}</span>
              <a :href="item.download_url" target="_blank">下载</a>
            </li>
          </ul>
        </div>
      </section>
    </main>

    <p class="error" v-if="errorText">{{ errorText }}</p>
  </div>
</template>
