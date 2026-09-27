<script lang="ts">
  // Port of the toast stack in upstream DashboardLayout.js.
  import { notifications, type NotificationType } from '../notifications'

  function toastStyle(type: NotificationType): { bar: string; tone: string; icon: string } {
    if (type === 'success') return { bar: 'border-l-success', tone: 'text-success', icon: 'check_circle' }
    if (type === 'error') return { bar: 'border-l-danger', tone: 'text-danger', icon: 'error' }
    if (type === 'warning') return { bar: 'border-l-warning', tone: 'text-warning', icon: 'warning' }
    return { bar: 'border-l-info', tone: 'text-info', icon: 'info' }
  }
</script>

<div class="fixed bottom-4 right-4 z-[80] flex w-[min(92vw,360px)] flex-col gap-2">
  {#each $notifications as n (n.id)}
    {@const style = toastStyle(n.type)}
    <div
      role={n.type === 'error' ? 'alert' : 'status'}
      class="rounded-brand border border-border-subtle border-l-[3px] bg-surface px-3 py-2.5 text-text-main shadow-elevated {style.bar}"
    >
      <div class="flex items-start gap-2.5">
        <span aria-hidden="true" class="material-symbols-outlined text-[18px] leading-5 {style.tone}">{style.icon}</span>
        <div class="min-w-0 flex-1">
          {#if n.title}<p class="mb-0.5 text-xs font-semibold {style.tone}">{n.title}</p>{/if}
          <p class="text-xs text-text-muted whitespace-pre-wrap break-words">{n.message}</p>
        </div>
        {#if n.dismissible}
          <button
            type="button"
            onclick={() => notifications.removeNotification(n.id)}
            class="text-text-subtle hover:text-text-main cursor-pointer"
            aria-label="Dismiss notification"
          >
            <span class="material-symbols-outlined text-[16px]">close</span>
          </button>
        {/if}
      </div>
    </div>
  {/each}
</div>
