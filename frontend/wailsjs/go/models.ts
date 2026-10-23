export namespace main {
	
	export class LaunchCommand {
	    id: string;
	    workspaceId: string;
	    mainEntryId: string;
	    name: string;
	    moduleDir: string;
	    modulePath: string;
	    mainDir: string;
	    mainFile: string;
	    envVars: Record<string, string>;
	    runArgs: string[];
	    logLevelPattern?: string;
	    lastPid: number;
	    status: string;
	    pid: number;
	    mode: string;
	    exitCode: number;
	
	    static createFrom(source: any = {}) {
	        return new LaunchCommand(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.mainEntryId = source["mainEntryId"];
	        this.name = source["name"];
	        this.moduleDir = source["moduleDir"];
	        this.modulePath = source["modulePath"];
	        this.mainDir = source["mainDir"];
	        this.mainFile = source["mainFile"];
	        this.envVars = source["envVars"];
	        this.runArgs = source["runArgs"];
	        this.logLevelPattern = source["logLevelPattern"];
	        this.lastPid = source["lastPid"];
	        this.status = source["status"];
	        this.pid = source["pid"];
	        this.mode = source["mode"];
	        this.exitCode = source["exitCode"];
	    }
	}
	export class LogLine {
	    stream: string;
	    line: string;
	    seq: number;
	
	    static createFrom(source: any = {}) {
	        return new LogLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stream = source["stream"];
	        this.line = source["line"];
	        this.seq = source["seq"];
	    }
	}
	export class MainEntry {
	    id: string;
	    workspaceId: string;
	    name: string;
	    moduleDir: string;
	    modulePath: string;
	    mainDir: string;
	    mainFile: string;
	
	    static createFrom(source: any = {}) {
	        return new MainEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.name = source["name"];
	        this.moduleDir = source["moduleDir"];
	        this.modulePath = source["modulePath"];
	        this.mainDir = source["mainDir"];
	        this.mainFile = source["mainFile"];
	    }
	}
	export class ProcessInfo {
	    pid: number;
	    executable: string;
	    commandLine: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new ProcessInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pid = source["pid"];
	        this.executable = source["executable"];
	        this.commandLine = source["commandLine"];
	        this.reason = source["reason"];
	    }
	}
	export class ShellSessionInfo {
	    id: string;
	    workspaceId: string;
	    commandId: string;
	    dir: string;
	    shell: string;
	    startedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new ShellSessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.commandId = source["commandId"];
	        this.dir = source["dir"];
	        this.shell = source["shell"];
	        this.startedAt = source["startedAt"];
	    }
	}
	export class legacyApp {
	    id: string;
	    workspaceId: string;
	    name: string;
	    moduleDir: string;
	    modulePath: string;
	    mainDir: string;
	    mainFile: string;
	    envVars: Record<string, string>;
	    runArgs: string[];
	    status: string;
	    pid: number;
	    mode: string;
	    exitCode: number;
	
	    static createFrom(source: any = {}) {
	        return new legacyApp(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.name = source["name"];
	        this.moduleDir = source["moduleDir"];
	        this.modulePath = source["modulePath"];
	        this.mainDir = source["mainDir"];
	        this.mainFile = source["mainFile"];
	        this.envVars = source["envVars"];
	        this.runArgs = source["runArgs"];
	        this.status = source["status"];
	        this.pid = source["pid"];
	        this.mode = source["mode"];
	        this.exitCode = source["exitCode"];
	    }
	}
	export class Workspace {
	    id: string;
	    name: string;
	    rootPath: string;
	    envVars: Record<string, string>;
	    logLevelPattern?: string;
	    createdAt: number;
	    mainEntries: MainEntry[];
	    commands: LaunchCommand[];
	    shellHistory?: string[];
	    shellFavorites?: string[];
	    apps?: legacyApp[];
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.rootPath = source["rootPath"];
	        this.envVars = source["envVars"];
	        this.logLevelPattern = source["logLevelPattern"];
	        this.createdAt = source["createdAt"];
	        this.mainEntries = this.convertValues(source["mainEntries"], MainEntry);
	        this.commands = this.convertValues(source["commands"], LaunchCommand);
	        this.shellHistory = source["shellHistory"];
	        this.shellFavorites = source["shellFavorites"];
	        this.apps = this.convertValues(source["apps"], legacyApp);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

