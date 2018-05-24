/* Global variables */

var appModule = angular.module('appModule',['ngRoute','ngAnimate'])

/*  Filters    */

// Tells if an object is instance of an array type. Used primary within ng-templates
appModule.filter('isArray', function() {
  return function (input) {
    return angular.isArray(input);
  };
});


// Add new item to list checking first if it has not being loaded and if it is not null.
// Used primary within ng-templates
appModule.filter('append', function() {
  return function (input, item) {
    if (item){
        for (i = 0; i < input.length; i++) {
            if(input[i] === item){
                return input;
            }
        }
        input.push(item);
    }
    return input;
  };
});

// Remove item from list. Used primary within ng-templates
appModule.filter('remove', function() {
  return function (input, item) {
    input.splice(input.indexOf(item),1);
    return input;
  };
});

// Capitalize the first letter of a word
appModule.filter('capitalize', function() {

  return function(token) {
      return token.charAt(0).toUpperCase() + token.slice(1);
   }
});

// Replace any especial character for a space
appModule.filter('removeSpecialCharacters', function() {

  return function(token) {
      return token.replace(/#|_|-|$|!|\*/g,' ').trim();
   }
});

/*  Configuration    */

// Application routing
appModule.config(function($routeProvider, $locationProvider){
    // Maps the URLs to the templates located in the server
    $routeProvider
        .when('/', {templateUrl: '/ng/home'})

        .when('/home', {templateUrl: '/ng/home'})
        .when('/topology', {templateUrl: '/ng/topology'})
        .when('/devices', {templateUrl: '/ng/devices'})
    $locationProvider.html5Mode(true);
});

// Add to all requests the authorization header
appModule.config(function ($httpProvider){

    $httpProvider.interceptors.push('authInterceptor');
});


appModule.filter('capitalize', function() {
    // Capitalize the first letter of a word
  return function(token) {
      return token.charAt(0).toUpperCase() + token.slice(1);
   }
});

// To avoid conflicts with other template tools such as Jinja2, all between {a a} will be managed by ansible instead of {{ }}
appModule.config(['$interpolateProvider', function($interpolateProvider) {
  $interpolateProvider.startSymbol('{a');
  $interpolateProvider.endSymbol('a}');
}]);

/* Factories */

// The notify factory allows services to notify to an specific controller when they finish operations
appModule.factory('NotifyingService' ,function($rootScope) {
    return {
        subscribe: function(scope, event_name, callback) {
            var handler = $rootScope.$on(event_name, callback);
            scope.$on('$destroy', handler);
        },

        notify: function(event_name) {
            $rootScope.$emit(event_name);
        }
    };
});

// The auth notify factory allows other components subscribe and being notified when authentication is successful
appModule.factory('AuthNotifyingService', function($rootScope) {
    return {
        subscribe: function(scope, callback) {
            var handler = $rootScope.$on('notifying-auth-event', callback);
            scope.$on('$destroy', handler);
        },

        notify: function() {
            $rootScope.$emit('notifying-auth-event');
        }
    };
});

// This factory adds the token to each API request
appModule.factory("authInterceptor", function($rootScope, $q, $window){
    return {
        request: function(config){
            config.headers = config.headers  || {};
            if ($window.sessionStorage.token){
                config.headers.Authorization = 'Bearer ' + $window.sessionStorage.token;
            }
            return config;
        },
        responseError: function(rejection){
            if (rejection.status === 401){
                //Manage common 401 actions
            }
            return $q.reject(rejection);
        }
    };
});

/*  Services    */

/* Authentication */
appModule.service("AuthService", function($window, $http, $location, AuthNotifyingService){
    function url_base64_decode(str){
        return window.atob(str)
    }

    this.url_base64_decode = url_base64_decode

    // if token is not stored, try to get it if not in login page
    if ($location.$$path != '/login'){
        if (!$window.sessionStorage.token){
            $http
            .get('api/token')
            .then(function (response, status, headers, config){
                $window.sessionStorage.token = response.data.token;
                AuthNotifyingService.notify();
            })
            .catch(function(response, status, headers, config){
                // Any issue go to login
                $window.location.href = '/login'
            })

        }
    }
})


/*  Controllers    */

appModule.controller('AuthController', function($scope, $http, $window, AuthService, AuthNotifyingService){



    $scope.logout = function() {
        $scope.isAuthenticated = false;
        $window.sessionStorage.token = '';
        $window.location.href = '/web/logout'
    }


    AuthNotifyingService.subscribe($scope, function updateToken() {
        $scope.token = $window.sessionStorage.token;
    });
});


//Location controller is in charge of managing the routing location of the application
appModule.controller('LocationController', function($scope, $location){
     $scope.go = function ( path ) {
        $location.path( path );
    };
});


// App controller is in charge of managing all services for the application
appModule.controller('AppController', function($scope, $location, $http){

    $scope.devices = []
    $scope.device = {}
    $scope.error = "";
    $scope.success = ""
    $scope.loading = false;
    $scope.isUpdate = false;

    $scope.clearError = function(){
        $scope.error = "";
    };
    $scope.clearSuccess = function(){
        $scope.success = "";
    };

    $scope.editDevice = function(pDevice){
        $scope.device = pDevice;
        $scope.isUpdate = true
    };

    $scope.newDevice = function(){
        $scope.device = {}
        $scope.isUpdate = false
    }

    $scope.sendDevice = function(){
        $scope.clearError();
        $scope.clearSuccess();

        if(!($scope.device.name && $scope.device.ip && $scope.device.username && $scope.device.password && $scope.device.port && $scope.device.certificate)){
            $scope.error = "Please complete all fields";
            return;
        }
        $scope.loading = true;
        $http
            .post('/api/device', $scope.device)
            .then(function (response, status, headers, config){
               $scope.success = "Device Added!"
               $scope.getDevices();
            })
            .catch(function(response, status, headers, config){
                $scope.error = response.data
            })
            .finally(function(){
                $scope.loading = false;
            })

    };

    $scope.deleteDevice = function(){
        $scope.clearError();
        $scope.clearSuccess();
        $scope.loading = true;
        $http
            .delete('/api/device?name=' + $scope.device.name)
            .then(function (response, status, headers, config){
               $scope.success = "Device Deleted!"
               $scope.getDevices();
               $scope.newDevice();
            })
            .catch(function(response, status, headers, config){
                $scope.error = response.data
            })
            .finally(function(){
                $scope.loading = false;
            })

    };

     $scope.getDevices = function(){
        $scope.loading = true;
        $http
            .get('/api/device')
            .then(function (response, status, headers, config){
                if(angular.isArray(response.data)){
                    $scope.devices = response.data;
                }
                else{
                    $scope.devices = [];
                }

            })
            .catch(function(response, status, headers, config){
                $scope.error = response.data
            })
            .finally(function(){
                $scope.loading = false;
            })
    };

    // Location logic. This tells the controller what to do according the URL that the user currently is
    $scope.$on('$viewContentLoaded', function(event) {
        if ($location.$$path === '/topology'){
            buildTopology(nx, nx.global, nxData);
        }
        if ($location.$$path === '/devices'){
            $scope.getDevices();
        }
    });
});
