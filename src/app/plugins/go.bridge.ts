
import { Plugin, registerPlugin } from '@capacitor/core';
import { SPNStatus, UserProfile } from '../lib/spn.types';

export interface GoBridgeInterface extends Plugin {
	IsTunnelActive(): Promise<any>
	EnableTunnel(): Promise<void>
	RestartTunnel(): Promise<void>
	SPNLogin(data: any): Promise<any>
	SPNLogout(): Promise<void>
	RefreshSPNUserProfile(): Promise<any>
	GetSPNUserProfile(): Promise<any>
	GetSPNStatus(): Promise<any>
	SetSPNEnabled(data: any): Promise<void>
	SetSPNExitCountry(data: any): Promise<void>
	GetSPNExitCountry(): Promise<any>
	GetTunnelLastError(): Promise<any>
	GetLogs(data: any): Promise<any>
	GetDebugInfoFile(): Promise<void>
	GetDebugInfo(): Promise<any>
	Shutdown(): Promise<void>
	CreateIssue(data: any): Promise<any>
	CreateTicket(data: any): Promise<void>
	IsGeoIPDataAvailable(): Promise<any>
	NewApkAvaliable(): Promise<any>
	PerformRequest(any): Promise<any>
	DatabaseMessage(data: any): Promise<void>
	SubscribeToDatabase(any): Promise<any>
	DownloadPendingUpdates(): Promise<void>
	DownloadUpdatesOnWifiConnected(): Promise<void>
	IsOnWifiNetwork(): Promise<any>

}
export const GoInterface = registerPlugin<GoBridgeInterface>("GoBridge")

export class GoBridgeClass {

	public IsTunnelActive(): Promise<boolean> {
		return new Promise<boolean>((resolve, reject) => {
            GoInterface.IsTunnelActive().then((result) => {
               resolve(result.ret0);
            }, (result) => {
               reject(result);
            });
        });
	}

	public EnableTunnel(): Promise<void> {
		return GoInterface.EnableTunnel();
	}

	public RestartTunnel(): Promise<void> {
		return GoInterface.RestartTunnel();
	}

	public SPNLogin(username: string, password: string): Promise<string> {
		return new Promise<string>((resolve, reject) => {
            GoInterface.SPNLogin({username: username, password: password}).then((result) => {
               resolve(result.ret0);
            }, (result) => {
               reject(result);
            });
        });
	}

	public SPNLogout(): Promise<void> {
		return GoInterface.SPNLogout();
	}

	public RefreshSPNUserProfile(): Promise<string> {
		return new Promise<string>((resolve, reject) => {
            GoInterface.RefreshSPNUserProfile().then((result) => {
               resolve(result.ret0);
            }, (result) => {
               reject(result);
            });
        });
	}

	public GetSPNUserProfile(): Promise<string> {
		return new Promise<string>((resolve, reject) => {
            GoInterface.GetSPNUserProfile().then((result) => {
               resolve(result.ret0);
            }, (result) => {
               reject(result);
            });
        });
	}

	public GetSPNStatus(): Promise<string> {
		return new Promise<string>((resolve, reject) => {
            GoInterface.GetSPNStatus().then((result) => {
               resolve(result.ret0);
            }, (result) => {
               reject(result);
            });
        });
	}

	public SetSPNEnabled(enabled: boolean): Promise<void> {
		return GoInterface.SetSPNEnabled({enabled: enabled});
	}

	public SetSPNExitCountry(country: string): Promise<void> {
		return GoInterface.SetSPNExitCountry({country: country});
	}

	public GetSPNExitCountry(): Promise<string> {
		return new Promise<string>((resolve, reject) => {
            GoInterface.GetSPNExitCountry().then((result) => {
               resolve(result.ret0);
            }, (result) => {
               reject(result);
            });
        });
	}

	public GetTunnelLastError(): Promise<string> {
		return new Promise<string>((resolve, reject) => {
            GoInterface.GetTunnelLastError().then((result) => {
               resolve(result.ret0);
            }, (result) => {
               reject(result);
            });
        });
	}

	public GetLogs(ID: number): Promise<any> {
		return new Promise<any>((resolve, reject) => {
            GoInterface.GetLogs({ID: ID}).then((result) => {
               resolve(result.ret0);
            }, (result) => {
               reject(result);
            });
        });
	}

	public GetDebugInfoFile(): Promise<void> {
		return GoInterface.GetDebugInfoFile();
	}

	public GetDebugInfo(): Promise<string> {
		return new Promise<string>((resolve, reject) => {
            GoInterface.GetDebugInfo().then((result) => {
               resolve(result.ret0);
            }, (result) => {
               reject(result);
            });
        });
	}

	public Shutdown(): Promise<void> {
		return GoInterface.Shutdown();
	}

	public CreateIssue(debugInfo: string, genUrl: boolean, issueRequestStr: string): Promise<string> {
		return new Promise<string>((resolve, reject) => {
            GoInterface.CreateIssue({debugInfo: debugInfo, genUrl: genUrl, issueRequestStr: issueRequestStr}).then((result) => {
               resolve(result.ret0);
            }, (result) => {
               reject(result);
            });
        });
	}

	public CreateTicket(debugInfo: string, ticketRequestStr: string): Promise<void> {
		return GoInterface.CreateTicket({debugInfo: debugInfo, ticketRequestStr: ticketRequestStr});
	}

	public IsGeoIPDataAvailable(): Promise<boolean> {
		return new Promise<boolean>((resolve, reject) => {
            GoInterface.IsGeoIPDataAvailable().then((result) => {
               resolve(result.ret0);
            }, (result) => {
               reject(result);
            });
        });
	}

	public NewApkAvaliable(): Promise<boolean> {
		return new Promise<boolean>((resolve, reject) => {
            GoInterface.NewApkAvaliable().then((result) => {
               resolve(result.ret0);
            }, (result) => {
               reject(result);
            });
        });
	}

	public PerformRequest(param: any): Promise<any> {
		return GoInterface.PerformRequest(param);
	}

	public DatabaseMessage(msg: string): Promise<void> {
		return GoInterface.DatabaseMessage({msg: msg});
	}

	public SubscribeToDatabase(param: any): Promise<any> {
		return GoInterface.SubscribeToDatabase(param);
	}

	public DownloadPendingUpdates(): Promise<void> {
		return GoInterface.DownloadPendingUpdates();
	}

	public DownloadUpdatesOnWifiConnected(): Promise<void> {
		return GoInterface.DownloadUpdatesOnWifiConnected();
	}

	public IsOnWifiNetwork(): Promise<boolean> {
		return new Promise<boolean>((resolve, reject) => {
            GoInterface.IsOnWifiNetwork().then((result) => {
               resolve(result.ret0);
            }, (result) => {
               reject(result);
            });
        });
	}

}

var GoBridge = new GoBridgeClass()
export default GoBridge;
