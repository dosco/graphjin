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

      <div class="bg-gray-100 mt-10">
        <div class="container mx-auto px-10 md:px-0 py-32">
          <h1 class="uppercase font-semibold text-xl text-blue-800 mb-4">
            What is {{ data.heroText }}?
          </h1>
          <div class="text-2xl md:text-3xl">
            Super Graph can automatically learn a Postgres database and instantly serve it as a fast and secured GraphQL API. It comes with tools to create a new app and manage it's database. You get it all, a very productive developer and a highly scalable app backend. It's designed to work well on serverless platforms by Google, AWS, Microsoft, etc. The goal is to save you a ton of time and money so you can focus on you're apps core value.
          </div>
        </div>
      </div>

      <div class="flex flex-wrap">
        <div class="md:w-2/4">
          <img src="/graphql.png">
        </div>

        <div class="md:w-2/4">
          <img src="/json.png">
        </div>
      </div>

      <div class="mt-10 py-10 md:py-20">
        <div class="container mx-auto px-10 md:px-0">
          <h1 class="uppercase font-semibold text-xl text-blue-800 mb-4">
            How to use {{ data.heroText }}?
          </h1>
          <div class="text-2xl md:text-3xl">
            <small class="text-sm">Use the below command to download and install Super Graph. You will need Go 1.13 or above</small>
            <pre>&#8227; GO111MODULE=on go get -u github.com/dosco/super-graph</pre>

            <small class="text-sm">Create a new app and change to it's directory</small>
            <pre>&#8227; super-graph new blog; cd blog</pre>

            <small class="text-sm">Setup the app database and seed it with fake data. Docker compose will start a Postgres database for your app</small>
            <pre>&#8227; docker-compose run blog_api ./super-graph db:setup</pre>

            <small class="text-sm">And finally launch Super Graph configured for your app</small>
            <pre>&#8227; docker-compose up</pre>
          </div>
        </div>
      </div>

      <div class="bg-gray-100 mt-10">
        <div class="container mx-auto px-10 md:px-0 py-32">
          <h1 class="uppercase font-semibold text-xl text-blue-800 mb-4">
            The story of {{ data.heroText }}
          </h1>
          <div class="text-2xl md:text-3xl">
            After working on several products through my career I find that we spend way too much time on building API backends. Most APIs also require constant updating, this costs real time and money.<br><br>
            
            It's always the same thing, figure out what the UI needs then build an endpoint for it. Most API code involves struggling with an ORM to query a database and mangle the data into a shape that the UI expects to see.<br><br>
            
            I didn't want to write this code anymore, I wanted the computer to do it. Enter GraphQL, to me it sounded great, but it still required me to write all the same database query code.<br><br>
            
            Having worked with compilers before I saw this as a compiler problem. Why not build a compiler that converts GraphQL to highly efficient SQL.<br><br>
            
            This compiler is what sits at the heart of Super Graph with layers of useful functionality around it like authentication, remote joins, rails integration, database migrations and everything else needed for you to build production ready apps with it.
          </div>
        </div>
      </div>

      <div class="overflow-hidden bg-indigo-900" style="height: 730px">
        <div class="container mx-auto py-20">
        <img src="/super-graph-web-ui.png">
        </div>
      </div>

      <div class="py-10 md:py-20">
        <div class="container mx-auto px-10 md:px-0">
          <h1 class="uppercase font-semibold text-xl text-blue-800 mb-4">
            Try it with a demo Rails app
          </h1>
          <div class="text-2xl md:text-3xl">
            <small class="text-sm">Download the Docker compose config for the demo</small>
            <pre>&#8227; curl -L -o demo.yml https://bit.ly/2mq05lW</pre>

            <small class="text-sm">Setup the demo database</small>
            <pre>&#8227; docker-compose -f demo.yml run rails_app rake db:create db:migrate db:seed</pre>

            <small class="text-sm">Run the demo</small>
            <pre>&#8227; docker-compose -f demo.yml up</pre>

            <small class="text-sm">Signin to the demo app (user1@demo.com / 123456)</small>
            <pre>&#8227; open http://localhost:3000</pre>

            <small class="text-sm">Try the super graph web ui</small>
            <pre>&#8227; open http://localhost:8080</pre>
          </div>
        </div>
      </div>

      <div class="border-t py-10">
        <div class="container mx-auto">
        <style>.embed-container { position: relative; padding-bottom: 56.25%; height: 0; overflow: hidden; max-width: 100%; } .embed-container iframe, .embed-container object, .embed-container embed { position: absolute; top: 0; left: 0; width: 100%; height: 100%; }</style>
        <div class="embed-container shadow">
          <iframe src='https://www.youtube.com/embed/MfPL2A-DAJk' frameborder='0' allowfullscreen></iframe>
          </div>
        </div>
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