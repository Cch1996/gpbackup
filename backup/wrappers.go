package backup

import "github.com/greenplum-db/gpbackup/utils"

/*
 * This file contains wrapper functions that group together functions relating
 * to querying and printing metadata, so that the logic for each object type
 * can all be in one place and backup.go can serve as a high-level look at the
 * overall backup flow.
 */

/*
 * Setup and validation wrapper functions
 */

func SetLoggerVerbosity() {
	if *quiet {
		logger.SetVerbosity(utils.LOGERROR)
	} else if *debug {
		logger.SetVerbosity(utils.LOGDEBUG)
	} else if *verbose {
		logger.SetVerbosity(utils.LOGVERBOSE)
	}
}

func InitializeConnection() {
	connection = utils.NewDBConn(*dbname)
	connection.Connect()
	connection.Exec("SET application_name TO 'gpbackup'")
	connection.Begin()
	connection.Exec("SET search_path TO pg_catalog")
}

func InitializeBackupReport() {
	backupReport = &utils.Report{
		DatabaseName:    connection.DBName,
		DatabaseVersion: connection.GetDatabaseVersion(),
		BackupVersion:   version,
		DatabaseSize:    connection.GetDBSize(),
	}
	backupReport.SetBackupTypeFromFlags(*dataOnly, *metadataOnly, includeSchemas)
}

/*
 * Metadata retrieval wrapper functions
 */

func RetrieveFunctionsAndTypes(objectCounts map[string]int, procLangs []ProceduralLanguage) ([]Function, []Function, []Type, MetadataMap, MetadataMap, map[uint32]FunctionInfo) {
	types := GetTypes(connection)
	objectCounts["Types"] = len(types)
	typeMetadata := GetMetadataForObjectType(connection, TYPE_TYPE)
	functions := GetFunctions(connection)
	objectCounts["Functions"] = len(functions)
	funcInfoMap := GetFunctionOidToInfoMap(connection)
	functionMetadata := GetMetadataForObjectType(connection, TYPE_FUNCTION)
	functions, types = ConstructFunctionAndTypeDependencyLists(connection, functions, types)
	langFuncs, otherFuncs := ExtractLanguageFunctions(functions, procLangs)
	return langFuncs, otherFuncs, types, functionMetadata, typeMetadata, funcInfoMap
}

func RetrieveAndLockTables(objectCounts map[string]int) ([]Relation, map[uint32]TableDefinition) {
	tables := GetAllUserTables(connection)
	LockTables(connection, tables)
	objectCounts["Tables"] = len(tables)
	tableDefs := ConstructDefinitionsForTables(connection, tables)
	return tables, tableDefs
}

func RetrieveConstraints(objectCounts map[string]int) ([]Constraint, MetadataMap) {
	constraints := GetConstraints(connection)
	conMetadata := GetCommentsForObjectType(connection, TYPE_CONSTRAINT)
	return constraints, conMetadata
}

/*
 * Generic metadata wrapper functions
 */

func LogBackupInfo() {
	logger.Info("Backup Timestamp = %s", globalCluster.Timestamp)
	logger.Info("Backup Database = %s", utils.QuoteIdent(connection.DBName))
	logger.Info("Backup Type = %s", backupReport.BackupType)
}

func BackupSessionGUCs(metadataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing session GUCs to global file")
	gucs := GetSessionGUCs(connection)
	PrintGlobalSessionGUCs(metadataFile, globalTOC, gucs)
}

/*
 * Global metadata wrapper functions
 */

func BackupTablespaces(globalFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE TABLESPACE statements to global file")
	tablespaces := GetTablespaces(connection)
	objectCounts["Tablespaces"] = len(tablespaces)
	tablespaceMetadata := GetMetadataForObjectType(connection, TYPE_TABLESPACE)
	PrintCreateTablespaceStatements(globalFile, globalTOC, tablespaces, tablespaceMetadata)
}

func BackupCreateDatabase(globalFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE DATABASE statement to global file")
	dbnames := GetDatabaseNames(connection)
	dbMetadata := GetMetadataForObjectType(connection, TYPE_DATABASE)
	PrintCreateDatabaseStatement(globalFile, globalTOC, connection.DBName, dbnames, dbMetadata, *backupGlobals)
}

func BackupDatabaseGUCs(globalFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing database GUCs to global file")
	databaseGucs := GetDatabaseGUCs(connection)
	objectCounts["Database GUCs"] = len(databaseGucs)
	PrintDatabaseGUCs(globalFile, globalTOC, databaseGucs, connection.DBName)
}

func BackupResourceQueues(globalFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE RESOURCE QUEUE statements to global file")
	resQueues := GetResourceQueues(connection)
	objectCounts["Resource Queues"] = len(resQueues)
	resQueueMetadata := GetCommentsForObjectType(connection, TYPE_RESOURCEQUEUE)
	PrintCreateResourceQueueStatements(globalFile, globalTOC, resQueues, resQueueMetadata)
}

func BackupRoles(globalFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE ROLE statements to global file")
	roles := GetRoles(connection)
	objectCounts["Roles"] = len(roles)
	roleMetadata := GetCommentsForObjectType(connection, TYPE_ROLE)
	PrintCreateRoleStatements(globalFile, globalTOC, roles, roleMetadata)
}

func BackupRoleGrants(globalFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing GRANT ROLE statements to global file")
	roleMembers := GetRoleMembers(connection)
	PrintRoleMembershipStatements(globalFile, globalTOC, roleMembers)
}

/*
 * Predata wrapper functions
 */

func BackupSchemas(predataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE SCHEMA statements to predata file")
	schemas := GetAllUserSchemas(connection)
	objectCounts["Schemas"] = len(schemas)
	schemaMetadata := GetMetadataForObjectType(connection, TYPE_SCHEMA)
	PrintCreateSchemaStatements(predataFile, globalTOC, schemas, schemaMetadata)
}

func BackupProceduralLanguages(predataFile *utils.FileWithByteCount, objectCounts map[string]int, procLangs []ProceduralLanguage, langFuncs []Function, functionMetadata MetadataMap, funcInfoMap map[uint32]FunctionInfo) {
	logger.Verbose("Writing CREATE PROCEDURAL LANGUAGE statements to predata file")
	objectCounts["Procedural Languages"] = len(procLangs)
	for _, langFunc := range langFuncs {
		PrintCreateFunctionStatement(predataFile, globalTOC, langFunc, functionMetadata[langFunc.Oid])
	}
	procLangMetadata := GetMetadataForObjectType(connection, TYPE_PROCLANGUAGE)
	PrintCreateLanguageStatements(predataFile, globalTOC, procLangs, funcInfoMap, procLangMetadata)
}

func BackupShellTypes(predataFile *utils.FileWithByteCount, objectCounts map[string]int, types []Type) {
	logger.Verbose("Writing CREATE TYPE statements for shell types to predata file")
	PrintCreateShellTypeStatements(predataFile, globalTOC, types)
}

func BackupEnumTypes(predataFile *utils.FileWithByteCount, objectCounts map[string]int, types []Type, typeMetadata MetadataMap) {
	logger.Verbose("Writing CREATE TYPE statements for enum types to predata file")
	PrintCreateEnumTypeStatements(predataFile, globalTOC, types, typeMetadata)
}

func BackupCreateSequences(predataFile *utils.FileWithByteCount, objectCounts map[string]int, sequences []Sequence, relationMetadata MetadataMap) {
	logger.Verbose("Writing CREATE SEQUENCE statements to predata file")
	objectCounts["Sequences"] = len(sequences)
	PrintCreateSequenceStatements(predataFile, globalTOC, sequences, relationMetadata)
}

// This function is fairly unwieldy, but there's not really a good way to break it down
func BackupFunctionsAndTypesAndTables(predataFile *utils.FileWithByteCount, otherFuncs []Function, types []Type, tables []Relation, functionMetadata MetadataMap, typeMetadata MetadataMap, relationMetadata MetadataMap, tableDefs map[uint32]TableDefinition, constraints []Constraint) {
	logger.Verbose("Writing CREATE FUNCTION statements to predata file")
	logger.Verbose("Writing CREATE TYPE statements for base, composite, and domain types to predata file")
	logger.Verbose("Writing CREATE TABLE statements to predata file")
	tables = ConstructTableDependencies(connection, tables)
	sortedSlice := SortFunctionsAndTypesAndTablesInDependencyOrder(otherFuncs, types, tables)
	filteredMetadata := ConstructFunctionAndTypeAndTableMetadataMap(functionMetadata, typeMetadata, relationMetadata)
	PrintCreateDependentTypeAndFunctionAndTablesStatements(predataFile, globalTOC, sortedSlice, filteredMetadata, tableDefs, constraints)
}

func BackupAlterSequences(predataFile *utils.FileWithByteCount, objectCounts map[string]int, sequences []Sequence) {
	logger.Verbose("Writing ALTER SEQUENCE statements to predata file")
	sequenceColumnOwners := GetSequenceColumnOwnerMap(connection)
	PrintAlterSequenceStatements(predataFile, globalTOC, sequences, sequenceColumnOwners)
}

func BackupProtocols(predataFile *utils.FileWithByteCount, objectCounts map[string]int, funcInfoMap map[uint32]FunctionInfo) {
	logger.Verbose("Writing CREATE PROTOCOL statements to predata file")
	protocols := GetExternalProtocols(connection)
	objectCounts["Protocols"] = len(protocols)
	protoMetadata := GetMetadataForObjectType(connection, TYPE_PROTOCOL)
	PrintCreateExternalProtocolStatements(predataFile, globalTOC, protocols, funcInfoMap, protoMetadata)
}

func BackupTSParsers(predataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE TEXT SEARCH PARSER statements to predata file")
	parsers := GetTextSearchParsers(connection)
	objectCounts["Text Search Parsers"] = len(parsers)
	parserMetadata := GetCommentsForObjectType(connection, TYPE_TSPARSER)
	PrintCreateTextSearchParserStatements(predataFile, globalTOC, parsers, parserMetadata)
}

func BackupTSTemplates(predataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE TEXT SEARCH TEMPLATE statements to predata file")
	templates := GetTextSearchTemplates(connection)
	objectCounts["Text Search Templates"] = len(templates)
	templateMetadata := GetCommentsForObjectType(connection, TYPE_TSTEMPLATE)
	PrintCreateTextSearchTemplateStatements(predataFile, globalTOC, templates, templateMetadata)
}

func BackupTSDictionaries(predataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE TEXT SEARCH DICTIONARY statements to predata file")
	dictionaries := GetTextSearchDictionaries(connection)
	objectCounts["Text Search Dictionaries"] = len(dictionaries)
	dictionaryMetadata := GetMetadataForObjectType(connection, TYPE_TSDICTIONARY)
	PrintCreateTextSearchDictionaryStatements(predataFile, globalTOC, dictionaries, dictionaryMetadata)
}

func BackupTSConfigurations(predataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE TEXT SEARCH CONFIGURATION statements to predata file")
	configurations := GetTextSearchConfigurations(connection)
	objectCounts["Text Search Configurations"] = len(configurations)
	configurationMetadata := GetMetadataForObjectType(connection, TYPE_TSCONFIGURATION)
	PrintCreateTextSearchConfigurationStatements(predataFile, globalTOC, configurations, configurationMetadata)
}

func BackupConversions(predataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE CONVERSION statements to predata file")
	conversions := GetConversions(connection)
	objectCounts["Conversions"] = len(conversions)
	convMetadata := GetMetadataForObjectType(connection, TYPE_CONVERSION)
	PrintCreateConversionStatements(predataFile, globalTOC, conversions, convMetadata)
}

func BackupOperators(predataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE OPERATOR statements to predata file")
	operators := GetOperators(connection)
	objectCounts["Operators"] = len(operators)
	operatorMetadata := GetMetadataForObjectType(connection, TYPE_OPERATOR)
	PrintCreateOperatorStatements(predataFile, globalTOC, operators, operatorMetadata)
}

func BackupOperatorFamilies(predataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE OPERATOR FAMILY statements to predata file")
	operatorFamilies := GetOperatorFamilies(connection)
	objectCounts["Operator Families"] = len(operatorFamilies)
	operatorFamilyMetadata := GetMetadataForObjectType(connection, TYPE_OPERATORFAMILY)
	PrintCreateOperatorFamilyStatements(predataFile, globalTOC, operatorFamilies, operatorFamilyMetadata)
}

func BackupOperatorClasses(predataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE OPERATOR CLASS statements to predata file")
	operatorClasses := GetOperatorClasses(connection)
	objectCounts["Operator Classes"] = len(operatorClasses)
	operatorClassMetadata := GetMetadataForObjectType(connection, TYPE_OPERATORCLASS)
	PrintCreateOperatorClassStatements(predataFile, globalTOC, operatorClasses, operatorClassMetadata)
}

func BackupAggregates(predataFile *utils.FileWithByteCount, objectCounts map[string]int, funcInfoMap map[uint32]FunctionInfo) {
	logger.Verbose("Writing CREATE AGGREGATE statements to predata file")
	aggregates := GetAggregates(connection)
	objectCounts["Aggregates"] = len(aggregates)
	aggMetadata := GetMetadataForObjectType(connection, TYPE_AGGREGATE)
	PrintCreateAggregateStatements(predataFile, globalTOC, aggregates, funcInfoMap, aggMetadata)
}

func BackupCasts(predataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE CAST statements to predata file")
	casts := GetCasts(connection)
	objectCounts["Casts"] = len(casts)
	castMetadata := GetCommentsForObjectType(connection, TYPE_CAST)
	PrintCreateCastStatements(predataFile, globalTOC, casts, castMetadata)
}

func BackupViews(predataFile *utils.FileWithByteCount, objectCounts map[string]int, relationMetadata MetadataMap) {
	logger.Verbose("Writing CREATE VIEW statements to predata file")
	views := GetViews(connection)
	objectCounts["Views"] = len(views)
	views = ConstructViewDependencies(connection, views)
	views = SortViews(views)
	PrintCreateViewStatements(predataFile, globalTOC, views, relationMetadata)
}

func BackupConstraints(predataFile *utils.FileWithByteCount, objectCounts map[string]int, constraints []Constraint, conMetadata MetadataMap) {
	logger.Verbose("Writing ADD CONSTRAINT statements to predata file")
	PrintConstraintStatements(predataFile, globalTOC, constraints, conMetadata)
}

/*
 * Postdata wrapper functions
 */

func BackupIndexes(postdataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE INDEX statements to postdata file")
	indexNameMap := ConstructImplicitIndexNames(connection)
	indexes := GetIndexes(connection, indexNameMap)
	objectCounts["Indexes"] = len(indexes)
	indexMetadata := GetCommentsForObjectType(connection, TYPE_INDEX)
	PrintCreateIndexStatements(postdataFile, globalTOC, indexes, indexMetadata)
}

func BackupRules(postdataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE RULE statements to postdata file")
	rules := GetRules(connection)
	objectCounts["Rules"] = len(rules)
	ruleMetadata := GetCommentsForObjectType(connection, TYPE_RULE)
	PrintCreateRuleStatements(postdataFile, globalTOC, rules, ruleMetadata)
}

func BackupTriggers(postdataFile *utils.FileWithByteCount, objectCounts map[string]int) {
	logger.Verbose("Writing CREATE TRIGGER statements to postdata file")
	triggers := GetTriggers(connection)
	objectCounts["Triggers"] = len(triggers)
	triggerMetadata := GetCommentsForObjectType(connection, TYPE_TRIGGER)
	PrintCreateTriggerStatements(postdataFile, globalTOC, triggers, triggerMetadata)
}

/*
 * Data wrapper functions
 */

func BackupData(tables []Relation, tableDefs map[uint32]TableDefinition) {
	numExtTables := 0
	for _, table := range tables {
		if !tableDefs[table.RelationOid].IsExternal {
			logger.Verbose("Writing data for table %s to file", table.ToString())
			backupFile := globalCluster.GetTableBackupFilePathForCopyCommand(table.RelationOid)
			CopyTableOut(connection, table, backupFile)
		} else {
			logger.Verbose("Skipping data backup of table %s because it is an external table.", table.ToString())
			numExtTables++
		}
	}
	if numExtTables > 0 {
		s := ""
		if numExtTables > 1 {
			s = "s"
		}
		logger.Warn("Skipped data backup of %d external table%s.", numExtTables, s)
		logger.Warn("See %s for a complete list of skipped tables.", logger.GetLogFileName())
	}
}
