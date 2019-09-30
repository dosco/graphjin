<template>
  <div>
    <main aria-labelledby="main-title" >
    <Navbar />

    <div class="container mx-auto">
      <div class="flex flex-col md:flex-row justify-between px-10 md:px-20">
        <div class="bg-bottom bg-no-repeat bg-cover">
          <div class="text-center md:text-left pt-24">
            <h1 v-if="data.heroText !== null" class="text-5xl font-bold text-black pb-0 uppercase">
              {{ data.heroText || $title || 'Hello' }}
            </h1>
            
            <p class="text-2xl text-gray-700 leading-tight pb-0">
              {{ data.tagline || $description || 'Welcome to your VuePress site' }}
            </p>

            <p class="text-lg text-gray-600 leading-tight">
              {{ data.longTagline }}
            </p>

            <NavLink
              class="inline-block px-4 py-3 my-8 bg-blue-600 text-blue-100 font-bold rounded"
              :item="actionLink"
            />
          </div>
        </div>

        <div class="py-10 md:p-20">
          <img src="/hologram.svg" class="h-64">
        </div>

      </div>
      </div>

      <div>
        <div
          class="flex flex-wrap mx-2 md:mx-20"
          v-if="data.features && data.features.length"
        >
          <div
            class="w-2/4 md:w-1/3 shadow"
            v-for="(feature, index) in data.features"
            :key="index"
          >
            <div class="p-8">
            <h2 class="md:text-xl text-blue-800 font-medium border-0 mb-1">{{ feature.title }}</h2>
            <p class="md:text-xl text-gray-700 leading-snug">{{ feature.details }}</p>
            </div>
          </div>
        </div>
      </div>

      <div class="bg-gray-100 my-10">
        <div class="container mx-auto px-10 md:px-0 py-32">
          <h1 class="uppercase font-semibold text-xl text-blue-800 mb-4">
            What is {{ data.heroText }}?
          </h1>
          <div class="text-2xl md:text-3xl">
            {{ data.description }}
          </div>
        </div>
      </div>

      <div class="theme-default-content markdown">
        <Content />
      </div>
        
      <div
        class="mx-auto text-center py-8"
        v-if="data.footer"
      >
        {{ data.footer }}
      </div>
    </main>
  </div>
</template>

<script>
import NavLink from '@theme/components/NavLink.vue'
import Navbar from '@theme/components/Navbar.vue'

export default {
  components: { NavLink, Navbar },

  computed: {
    data () {
      return this.$page.frontmatter
    },

    actionLink () {
      return {
        link: this.data.actionLink,
        text: this.data.actionText
      }
    }
  }
}
</script>