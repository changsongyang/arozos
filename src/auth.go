package main

import (
	"crypto/rand"
	"encoding/json"
	"net/http"

	auth "imuslab.com/arozos/mod/auth"
	prout "imuslab.com/arozos/mod/prouter"
	"imuslab.com/arozos/mod/utils"
)

func AuthInit() {
	//Generate session key for authentication module if empty
	sysdb.NewTable("auth")
	if *session_key == "" {
		//Check if the key was generated already. If not, generate a new one
		if !sysdb.KeyExists("auth", "sessionkey") {
			key := make([]byte, 32)
			rand.Read(key)
			newSessionKey := string(key)
			sysdb.Write("auth", "sessionkey", newSessionKey)
			systemWideLogger.PrintAndLog("Auth", "New authentication session key generated", nil)
		} else {
			systemWideLogger.PrintAndLog("Auth", "Authentication session key loaded from database", nil)

		}
		skeyString := ""
		sysdb.Read("auth", "sessionkey", &skeyString)
		session_key = &skeyString
	}

	//Create an Authentication Agent
	authAgent = auth.NewAuthenticationAgent("ao_auth", []byte(*session_key), sysdb, *allow_public_registry, func(w http.ResponseWriter, r *http.Request) {
		//Login Redirection Handler, redirect it login.system
		w.Header().Set("Cache-Control", "no-cache, no-store, no-transform, must-revalidate, private, max-age=0")
		http.Redirect(w, r, utils.ConstructRelativePathFromRequestURL(r.RequestURI, "login.system")+"?redirect="+r.URL.Path, http.StatusTemporaryRedirect)
	})

	if *allow_autologin {
		authAgent.AllowAutoLogin = true
	} else {
		//Default is false. But just in case
		authAgent.AllowAutoLogin = false
	}

	//Register the API endpoints for the authentication UI
	http.HandleFunc("/system/auth/login", authAgent.HandleLogin)
	http.HandleFunc("/system/auth/logout", authAgent.HandleLogout)
	http.HandleFunc("/system/auth/register", authAgent.HandleRegister)
	http.HandleFunc("/system/auth/checkLogin", authAgent.CheckLogin)
	http.HandleFunc("/api/auth/login", authAgent.HandleAutologinTokenLogin)

	authAgent.LoadAutologinTokenFromDB()
}

func AuthSettingsInit() {
	//Authentication related settings
	adminRouter := prout.NewModuleRouter(prout.RouterOption{
		ModuleName:  "System Setting",
		AdminOnly:   true,
		UserHandler: userHandler,
		DeniedHandler: func(w http.ResponseWriter, r *http.Request) {
			utils.SendErrorResponse(w, "Permission Denied")
		},
	})

	//Handle additional batch operations
	adminRouter.HandleFunc("/system/auth/csvimport", authAgent.HandleCreateUserAccountsFromCSV)
	adminRouter.HandleFunc("/system/auth/groupdel", authAgent.HandleUserDeleteByGroup)

	//System for logging and displaying login user information
	registerSetting(settingModule{
		Name:         "Connection Log",
		Desc:         "Logs for login attempts",
		IconPath:     "SystemAO/security/img/small_icon.png",
		Group:        "Security",
		StartDir:     "SystemAO/security/connlog.html",
		RequireAdmin: true,
	})

	adminRouter.HandleFunc("/system/auth/logger/index", authAgent.Logger.HandleIndexListing)
	adminRouter.HandleFunc("/system/auth/logger/list", authAgent.Logger.HandleTableListing)

	//Blacklist Management
	registerSetting(settingModule{
		Name:         "Access Control",
		Desc:         "Prevent / Allow certain IP ranges from logging in",
		IconPath:     "SystemAO/security/img/small_icon.png",
		Group:        "Security",
		StartDir:     "SystemAO/security/accesscontrol.html",
		RequireAdmin: true,
	})

	//Whitelist API
	adminRouter.HandleFunc("/system/auth/whitelist/enable", authAgent.WhitelistManager.HandleSetWhitelistEnable)
	adminRouter.HandleFunc("/system/auth/whitelist/list", authAgent.WhitelistManager.HandleListWhitelistedIPs)
	adminRouter.HandleFunc("/system/auth/whitelist/set", authAgent.WhitelistManager.HandleAddWhitelistedIP)
	adminRouter.HandleFunc("/system/auth/whitelist/unset", authAgent.WhitelistManager.HandleRemoveWhitelistedIP)

	//Blacklist API
	adminRouter.HandleFunc("/system/auth/blacklist/enable", authAgent.BlacklistManager.HandleSetBlacklistEnable)
	adminRouter.HandleFunc("/system/auth/blacklist/list", authAgent.BlacklistManager.HandleListBannedIPs)
	adminRouter.HandleFunc("/system/auth/blacklist/ban", authAgent.BlacklistManager.HandleAddBannedIP)
	adminRouter.HandleFunc("/system/auth/blacklist/unban", authAgent.BlacklistManager.HandleRemoveBannedIP)

	//Register nightly task for clearup all user retry counter
	nightlyManager.RegisterNightlyTask(authAgent.ExpDelayHandler.ResetAllUserRetryCounter)

	//Register nightly task for clearup all expired switchable account pools
	nightlyManager.RegisterNightlyTask(authAgent.SwitchableAccountManager.RunNightlyCleanup)

	/*
		Account switching functions
	*/

	//Register the APIs for account switching functions
	userRouter := prout.NewModuleRouter(prout.RouterOption{
		AdminOnly:   false,
		UserHandler: userHandler,
		DeniedHandler: func(w http.ResponseWriter, r *http.Request) {
			utils.SendErrorResponse(w, "Permission Denied")
		},
	})

	userRouter.HandleFunc("/system/auth/u/list", authAgent.SwitchableAccountManager.HandleSwitchableAccountListing)
	userRouter.HandleFunc("/system/auth/u/switch", authAgent.SwitchableAccountManager.HandleAccountSwitch)
	userRouter.HandleFunc("/system/auth/u/logoutAll", authAgent.SwitchableAccountManager.HandleLogoutAllAccounts)

	//API for not logged in pool check
	http.HandleFunc("/system/auth/u/p/list", func(w http.ResponseWriter, r *http.Request) {
		type ResumableSessionAccount struct {
			Username     string
			ProfileImage string
		}
		resp := ResumableSessionAccount{}
		sessionOwnerName := authAgent.SwitchableAccountManager.GetUnauthedSwitchableAccountCreatorList(w, r)
		resp.Username = sessionOwnerName
		if sessionOwnerName != "" {
			u, err := userHandler.GetUserInfoFromUsername(sessionOwnerName)
			if err == nil {
				resp.ProfileImage = u.GetUserIcon()
			}
		}

		js, _ := json.Marshal(resp)
		utils.SendJSONResponse(w, string(js))
	})
}

// Validate secure request that use authreq.html
// Require POST: password and admin permission
// return true if authentication passed
func AuthValidateSecureRequest(w http.ResponseWriter, r *http.Request, requireAdmin bool) bool {
	userinfo, err := userHandler.GetUserInfoFromRequest(w, r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("401 Unauthorized"))
		return false
	}

	if requireAdmin {
		if !userinfo.IsAdmin() {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("403 Forbidden"))
			return false
		}
	}

	//Double check password for this user
	password, err := utils.PostPara(r, "password")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("401 Unauthorized"))
		return false
	}

	passwordCorrect, rejectionReason := authAgent.ValidateUsernameAndPasswordWithReason(userinfo.Username, password)
	if !passwordCorrect {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(rejectionReason))
		return false
	}

	return true
}
