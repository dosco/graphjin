Rails.application.routes.draw do
  devise_for :customers
  devise_for :users
  resources :products
  # For details on the DSL available within this file, see http://guides.rubyonrails.org/routing.html

  get '/stripe/:id', to: 'stripe#show', as: 'stripe'

  root to: "products#index"
end
