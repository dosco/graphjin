<template>
  <div class="container mx-auto" style="background-color: #f3f9fd;">
  <div class="mt-16">
  <main aria-labelledby="main-title" >
    <Navbar
      @toggle-sidebar="toggleSidebar"
    />

    <div>
      <div class="p-4 pt-12 border-white border-b-4">
        <div class="text-center">
          <h1 v-if="data.heroText !== null" class="text-5xl">{{ data.heroText || $title || 'Hello' }}</h1>
          
          <p class="text-2xl text-gray-600 leading-tight mt-4 xl:mt-0">
            {{ data.tagline || $description || 'Welcome to your VuePress site' }}
          </p>

          <NavLink
            class="inline-block px-4 py-2 my-8 bg-green-500 text-green-100 font-bold rounded"
            :item="actionLink"
          />
        </div>

        <div
          class="flex flex-wrap mx-2 md:mx-20"
          v-if="data.features && data.features.length"
        >
          <div
            class="w-2/4 md:w-1/3"
            v-for="(feature, index) in data.features"
            :key="index"
          >
            <div class="p-4 pl-0 pb-8">
            <h2 class="md:text-2xl font-medium border-0 mb-2">{{ feature.title }}</h2>
            <p class="md:text-xl text-gray-600 leading-snug">{{ feature.details }}</p>
            </div>
          </div>
        </div>
      </div>

      <div class="border-white border-b-4">
       <img
          v-if="data.heroImage"
          :src="$withBase(data.heroImageMobile)"
          :alt="data.heroAlt || 'hero'"
        >
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