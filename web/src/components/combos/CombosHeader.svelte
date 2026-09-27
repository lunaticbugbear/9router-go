<script lang="ts">
  import Button from '../../lib/ui/Button.svelte'
  import PageHeader from '../../lib/ui/PageHeader.svelte'

  interface Props {
    onCreateClick: () => void
  }

  let { onCreateClick }: Props = $props()
</script>

<PageHeader tab="combos">
  {#snippet actions()}
    <Button icon="add" onclick={onCreateClick}>Create combo</Button>
  {/snippet}
</PageHeader>
