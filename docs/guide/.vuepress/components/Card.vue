<template>
<div class="shadow p-4 flex items-start" :class="className">
  <slot name="image"></slot>
  <div class="pl-4">
    <h2 class="p-0">
      <slot name="title"></slot>
    </h2>
    <p>
      <slot name="body"></slot>
    </p>
  </div>
</div>
</template>

<script>
export default {
  props: ["className"]
}
</script>