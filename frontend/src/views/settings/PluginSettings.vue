<template>
  <div class="plugin-settings">
    <div class="section-header">
      <h2>{{ t('settings.pluginManagement', '插件管理') }}</h2>
      <p class="section-description">
        {{ t('settings.pluginManagementDesc', '查看已加载的外部插件及其健康状态；数据源插件还可以控制是否接收新的同步请求。') }}
      </p>
    </div>

    <div v-if="loading" class="loading-state">
      <t-loading size="small" />
      <span>{{ t('settings.pluginLoading', '正在加载插件状态…') }}</span>
    </div>

    <div v-else-if="error" class="error-inline">
      <t-alert theme="error" :message="error">
        <template #operation>
          <t-button size="small" @click="load">{{ t('settings.retry', '重试') }}</t-button>
        </template>
      </t-alert>
    </div>

    <template v-else>
      <div v-if="plugins.length === 0" class="empty-state">
        <t-icon name="plugin" size="40px" class="empty-icon" />
        <p class="empty-text">{{ t('settings.noExternalPlugins', '暂未加载外部插件') }}</p>
        <p class="empty-hint">
          {{ t('settings.pluginInstallHint', '将插件 manifest.json 放入 WEKNORA_PLUGIN_DIR 的子目录后重启 WeKnora。') }}
        </p>
      </div>

      <div v-else class="plugin-list">
        <article v-for="plugin in plugins" :key="`${plugin.extensionType}:${plugin.type}`" class="plugin-card">
          <div class="plugin-card__top">
            <div class="plugin-card__identity">
              <div class="plugin-card__badge">{{ initial(plugin.name) }}</div>
              <div>
                <h3>{{ plugin.name }}</h3>
                <p>{{ plugin.type }}</p>
              </div>
            </div>
            <div class="plugin-card__actions">
              <span :class="['status', plugin.healthy ? 'status--ok' : 'status--error']">
                <span class="status__dot" />
                {{ plugin.healthy ? t('settings.pluginHealthy', '健康') : t('settings.pluginUnhealthy', '异常') }}
              </span>
              <t-switch
                v-if="plugin.canToggle"
                :model-value="plugin.enabled"
                :loading="plugin.updating"
                :disabled="plugin.updating"
                @change="(value: boolean) => toggle(plugin.type, value)"
              />
              <span v-else class="managed-label">
                {{ plugin.extensionType === 'web_search' ? t('settings.pluginManagedBySearch', '由搜索服务管理') : plugin.extensionType === 'model_provider' ? t('settings.pluginManagedByModel', '由模型服务管理') : t('settings.pluginManagedByService', '由解析服务管理') }}
              </span>
            </div>
          </div>

          <p class="plugin-card__description">{{ plugin.description || plugin.type }}</p>
          <div class="plugin-card__meta">
            <span>{{ t('settings.pluginExtension', '扩展类型') }}：{{ plugin.extensionType === 'document_parser' ? t('settings.documentParser', '文档解析') : plugin.extensionType === 'web_search' ? t('settings.webSearch', '网络搜索') : plugin.extensionType === 'model_provider' ? t('settings.modelProvider', '模型厂商') : t('settings.datasource', '数据源') }}</span>
            <span>{{ t('settings.pluginNetwork', '联网') }}：{{ plugin.allowNetwork ? t('settings.allowed', '允许') : t('settings.denied', '禁止') }}</span>
            <span v-if="plugin.readPaths.length > 0">
              {{ t('settings.pluginReadPaths', '读取目录') }}：{{ plugin.readPaths.join('、') }}
            </span>
          </div>
          <p v-if="plugin.error" class="plugin-card__error">{{ plugin.error }}</p>
        </article>
      </div>

      <p class="plugin-settings__note">
        {{ t('settings.pluginLifecycleNote', '安装、卸载和版本更新仍通过插件目录与 Docker 镜像完成；此页面管理已加载插件的运行状态。') }}
      </p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import {
  getConnectorStatuses,
  getConnectorTypes,
  setConnectorEnabled,
  type ConnectorMeta,
  type ConnectorStatus,
} from '@/api/datasource'
import { getParserEngines, type ParserEngineInfo } from '@/api/system'
import {
  listWebSearchProviderStatuses,
  listWebSearchProviderTypes,
  type WebSearchProviderStatus,
  type WebSearchProviderTypeInfo,
} from '@/api/web-search-provider'
import { listModelProviders, listModelProviderStatuses, type ModelProviderOption, type ModelProviderStatus } from '@/api/initialization'

const { t } = useI18n()
const loading = ref(true)
const error = ref('')
const metas = ref<ConnectorMeta[]>([])
const statuses = ref<ConnectorStatus[]>([])
const parserEngines = ref<ParserEngineInfo[]>([])
const searchMetas = ref<WebSearchProviderTypeInfo[]>([])
const searchStatuses = ref<WebSearchProviderStatus[]>([])
const modelMetas = ref<ModelProviderOption[]>([])
const modelStatuses = ref<ModelProviderStatus[]>([])
const updating = ref(new Set<string>())

const plugins = computed(() => {
  const statusMap = new Map(statuses.value.map(status => [status.type, status]))
  const datasourcePlugins = metas.value
    .filter(meta => meta.external)
    .map(meta => {
      const status = statusMap.get(meta.type)
      return {
        ...meta,
        enabled: status?.enabled ?? false,
        healthy: status?.healthy ?? false,
        error: status?.error || '',
        allowNetwork: meta.permissions?.allow_network ?? false,
        readPaths: meta.permissions?.read_paths || [],
        updating: updating.value.has(meta.type),
        extensionType: 'datasource',
        canToggle: true,
      }
    })
  const parserPlugins = parserEngines.value
    .filter(engine => engine.External)
    .map(engine => ({
      type: engine.Name,
      name: engine.Name,
      description: engine.Description,
      enabled: true,
      healthy: engine.Available ?? false,
      error: engine.UnavailableReason || '',
      allowNetwork: false,
      readPaths: [],
      updating: false,
      extensionType: 'document_parser',
      canToggle: false,
    }))
  const searchStatusMap = new Map(searchStatuses.value.map(status => [status.type, status]))
  const searchPlugins = searchMetas.value
    .filter(meta => meta.external)
    .map(meta => {
      const status = searchStatusMap.get(meta.id)
      return {
        type: meta.id,
        name: meta.name,
        description: meta.description,
        enabled: true,
        healthy: status?.healthy ?? false,
        error: status?.error || '',
        allowNetwork: meta.permissions?.allow_network ?? false,
        readPaths: meta.permissions?.read_paths || [],
        updating: false,
        extensionType: 'web_search',
        canToggle: false,
      }
    })
  const modelStatusMap = new Map(modelStatuses.value.map(status => [status.type, status]))
  const modelPlugins = modelMetas.value
    .filter(meta => meta.external)
    .map(meta => {
      const status = modelStatusMap.get(meta.value)
      return {
        type: meta.value,
        name: meta.label,
        description: meta.description,
        enabled: true,
        healthy: status?.healthy ?? false,
        error: status?.error || '',
        allowNetwork: meta.permissions?.allow_network ?? false,
        readPaths: meta.permissions?.read_paths || [],
        updating: false,
        extensionType: 'model_provider',
        canToggle: false,
      }
    })
  return [...datasourcePlugins, ...parserPlugins, ...searchPlugins, ...modelPlugins]
})

function unwrap<T>(value: T | { data?: T }): T {
  if (value && typeof value === 'object' && 'data' in value && (value as { data?: T }).data !== undefined) {
    return (value as { data: T }).data
  }
  return value as T
}

function initial(name: string) {
  return (name.trim().charAt(0) || '?').toUpperCase()
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [metaResponse, statusResponse, parserResponse, webSearchTypes, webSearchStatuses, modelProviders, modelProviderStatuses] = await Promise.all([
      getConnectorTypes(), getConnectorStatuses(), getParserEngines(),
      listWebSearchProviderTypes(), listWebSearchProviderStatuses(),
      listModelProviders(), listModelProviderStatuses(),
    ])
    metas.value = unwrap(metaResponse as ConnectorMeta[]) || []
    statuses.value = unwrap(statusResponse as ConnectorStatus[]) || []
    parserEngines.value = parserResponse?.data || []
    searchMetas.value = webSearchTypes || []
    searchStatuses.value = webSearchStatuses || []
    modelMetas.value = modelProviders || []
    modelStatuses.value = modelProviderStatuses || []
  } catch (err: any) {
    error.value = err?.message || t('settings.pluginLoadFailed', '插件状态加载失败')
  } finally {
    loading.value = false
  }
}

async function toggle(type: string, enabled: boolean) {
  updating.value = new Set([...updating.value, type])
  try {
    await setConnectorEnabled(type, enabled)
    await load()
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('settings.pluginUpdateFailed', '插件状态更新失败'))
    await load()
  } finally {
    const next = new Set(updating.value)
    next.delete(type)
    updating.value = next
  }
}

onMounted(load)
</script>

<style scoped>
.plugin-settings { max-width: 900px; }
.plugin-list { display: grid; gap: 14px; }
.plugin-card {
  padding: 18px 20px;
  border: 1px solid var(--td-component-border);
  border-radius: 12px;
  background: var(--td-bg-color-container);
}
.plugin-card__top, .plugin-card__identity, .plugin-card__actions { display: flex; align-items: center; }
.plugin-card__top { justify-content: space-between; gap: 16px; }
.plugin-card__identity { gap: 12px; min-width: 0; }
.plugin-card__identity h3 { margin: 0; font-size: 16px; color: var(--td-text-color-primary); }
.plugin-card__identity p { margin: 4px 0 0; color: var(--td-text-color-secondary); font-size: 12px; }
.plugin-card__badge {
  display: grid; place-items: center; width: 38px; height: 38px; border-radius: 10px;
  color: var(--td-brand-color); background: var(--td-brand-color-light); font-weight: 700;
}
.plugin-card__actions { gap: 18px; }
.managed-label { color: var(--td-text-color-secondary); font-size: 12px; }
.status { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; }
.status__dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
.status--ok { color: var(--td-success-color); }
.status--error { color: var(--td-error-color); }
.plugin-card__description { margin: 14px 0 10px; color: var(--td-text-color-secondary); }
.plugin-card__meta { display: flex; flex-wrap: wrap; gap: 8px 18px; color: var(--td-text-color-placeholder); font-size: 12px; }
.plugin-card__error { margin: 10px 0 0; color: var(--td-error-color); font-size: 12px; }
.plugin-settings__note, .empty-hint { color: var(--td-text-color-secondary); font-size: 13px; }
.plugin-settings__note { margin-top: 18px; }
.empty-state { padding: 48px 20px; text-align: center; }
.empty-icon { color: var(--td-text-color-placeholder); }
.empty-text { margin: 12px 0 8px; color: var(--td-text-color-primary); }
</style>
