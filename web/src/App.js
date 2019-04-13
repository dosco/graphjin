import React, { Component } from 'react'
import { Provider } from 'react-redux'
import { Playground, store } from 'graphql-playground-react'

import './index.css'

class App extends Component {
  render() {
    return (
      <div>
        <header style={{
          background: '#4d2692', 
          color: '#f1e9ff',
          letterSpacing: '0.15rem',
          textTransform: 'uppercase'
          }}
        >
          <div style={{
            padding: '1.45rem 1.0875rem'
          }}>
          
          <h3 style={{
            textDecoration: 'none',
            margin: '0px',
            fontSize: '18px'
            }}
          >
          Super Graph</h3>
          </div>
        </header>

        <Provider store={store}>
        <Playground 
          endpoint="/api/v1/graphql"
          settings={{
            'schema.polling.enable': false,
            'request.credentials': 'include',
            'general.betaUpdates': true,
            'editor.reuseHeaders': true,
            'editor.theme': 'dark'
          }} 
        />
        </Provider>
      </div>
    );
  }
}

export default App;
